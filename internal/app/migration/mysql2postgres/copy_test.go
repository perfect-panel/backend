package mysql2postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/mattn/go-sqlite3"
)

// COPY's text format delimits fields with tabs and rows with newlines, so
// those characters and the escape character itself must be escaped, and
// NULL has its own marker.
func TestCopyTextEscapesDelimiters(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 30, 0, 123000000, time.UTC)
	for _, tc := range []struct {
		value any
		want  string
	}{
		{nil, `\N`},
		{"plain", "plain"},
		{"tab\there", `tab\there`},
		{"line\nbreak\r", `line\nbreak\r`},
		{`back\slash`, `back\\slash`},
		{[]byte{0xde, 0xad}, `\\xdead`},
		{true, "t"},
		{false, "f"},
		{int64(-42), "-42"},
		{uint64(18446744073709551615), "18446744073709551615"},
		{1.5, "1.5"},
		{float32(0.25), "0.25"},
		{at, "2026-09-28 10:30:00.123Z"},
	} {
		if got := copyText(tc.value); got != tc.want {
			t.Errorf("copyText(%#v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestCopyStatementQuotesIdentifiers(t *testing.T) {
	got := copyStatement("public", `we"ird`, []string{"id", "user"})
	want := `COPY "public"."we""ird" ("id", "user") FROM STDIN`
	if got != want {
		t.Fatalf("copyStatement = %q, want %q", got, want)
	}
}

// The copy runs end to end against the CI databases: every value kind the
// conversion produces must reach PostgreSQL unchanged.
func TestMigrateCopiesRowsIntoPostgres(t *testing.T) {
	mysqlDSN, postgresDSN := os.Getenv("PPANEL_TEST_MYSQL_DSN"), os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if mysqlDSN == "" || postgresDSN == "" {
		t.Skip("set PPANEL_TEST_MYSQL_DSN and PPANEL_TEST_POSTGRES_DSN to run the MySQL to PostgreSQL copy test")
	}
	ctx := context.Background()
	table := fmt.Sprintf("m2p_copy_%d", time.Now().UnixNano())

	mysqlDB, err := sql.Open("mysql", normalizedSourceDSN(t, mysqlDSN, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDatabase("mysql", mysqlDB) })
	postgresDB, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDatabase("postgres", postgresDB) })
	schema := postgresTestSchema(t, postgresDB)
	target := quotePGIdent(schema) + "." + quotePGIdent(table)

	exec := func(db *sql.DB, query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(mysqlDB, "CREATE TABLE "+quoteMySQLIdent(table)+" (id BIGINT PRIMARY KEY, note TEXT NULL, flag TINYINT(1) NOT NULL, amount DECIMAL(10,2) NOT NULL, at DATETIME(3) NULL)")
	t.Cleanup(func() { _, _ = mysqlDB.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteMySQLIdent(table)) })
	exec(postgresDB, "CREATE TABLE "+target+" (id bigint PRIMARY KEY, note text, flag boolean NOT NULL, amount numeric(10,2) NOT NULL, at timestamp(3))")

	at := time.Date(2026, 9, 28, 10, 30, 0, 123000000, time.UTC)
	notes := []any{"tab\tnew\nline \\ backslash", nil, "0000-00-00 was the original date"}
	for i, note := range notes {
		var stamp any = at.Add(time.Duration(i) * time.Hour)
		if i == 1 {
			stamp = nil
		}
		exec(mysqlDB, "INSERT INTO "+quoteMySQLIdent(table)+" (id, note, flag, amount, at) VALUES (?, ?, ?, ?, ?)", i+1, note, i%2, fmt.Sprintf("%d.%02d", i+10, i), stamp)
	}

	if err := Migrate(ctx, Config{MySQLDSN: mysqlDSN, PostgresDSN: postgresDSN, Schema: schema, Tables: table, BatchSize: 2, Location: "UTC"}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := postgresDB.QueryContext(ctx, "SELECT id, note, flag, amount::text, at FROM "+target+" ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got int
	for rows.Next() {
		var (
			id     int64
			note   sql.NullString
			flag   bool
			amount string
			stamp  sql.NullTime
		)
		if err := rows.Scan(&id, &note, &flag, &amount, &stamp); err != nil {
			t.Fatal(err)
		}
		i := int(id) - 1
		if want, _ := notes[i].(string); note.Valid != (notes[i] != nil) || note.String != want {
			t.Errorf("row %d note = %#v, want %#v", id, note, notes[i])
		}
		if flag != (i%2 == 1) {
			t.Errorf("row %d flag = %v", id, flag)
		}
		if want := fmt.Sprintf("%d.%02d", i+10, i); amount != want {
			t.Errorf("row %d amount = %s, want %s", id, amount, want)
		}
		if i == 1 && stamp.Valid {
			t.Errorf("row %d timestamp = %v, want NULL", id, stamp.Time)
		}
		if want := at.Add(time.Duration(i) * time.Hour); i != 1 && (!stamp.Valid || !stamp.Time.Equal(want)) {
			t.Errorf("row %d timestamp = %v, want %v", id, stamp.Time, want)
		}
		got++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got != len(notes) {
		t.Fatalf("copied %d rows, want %d", got, len(notes))
	}
}

// copyTable streams a source table into PostgreSQL. SQLite stands in for
// MySQL here (it accepts the backtick quoting the source query uses), so the
// PostgreSQL side runs wherever PPANEL_TEST_POSTGRES_DSN is set.
func TestCopyTableIntoPostgres(t *testing.T) {
	postgresDSN := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if postgresDSN == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run the PostgreSQL copy test")
	}
	ctx := context.Background()
	table := fmt.Sprintf("m2p_stream_%d", time.Now().UnixNano())

	source, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer closeDatabase("sqlite", source)
	source.SetMaxOpenConns(1)
	postgresDB, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDatabase("postgres", postgresDB) })

	if _, err := source.ExecContext(ctx, "CREATE TABLE "+quoteMySQLIdent(table)+" (id INTEGER, note TEXT, flag INTEGER, amount TEXT, at TEXT)"); err != nil {
		t.Fatal(err)
	}
	notes := []any{"tab\tnew\nline \\ backslash", nil, "plain"}
	for i, note := range notes {
		var stamp any = fmt.Sprintf("2026-09-28 1%d:30:00.123", i)
		if i == 1 {
			stamp = nil
		}
		if _, err := source.ExecContext(ctx, "INSERT INTO "+quoteMySQLIdent(table)+" VALUES (?, ?, ?, ?, ?)", i+1, note, i%2, fmt.Sprintf("%d.%02d", i+10, i), stamp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := postgresDB.ExecContext(ctx, "CREATE TABLE public."+quotePGIdent(table)+" (id bigint, note text, flag boolean NOT NULL, amount numeric(10,2) NOT NULL, at timestamp(3))"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := postgresDB.ExecContext(ctx, "DROP TABLE IF EXISTS public."+quotePGIdent(table)); err != nil {
			t.Error(err)
		}
	})

	plan := tablePlan{Name: table, RowCount: int64(len(notes)), OrderColumns: []string{"id"}, Columns: []postgresColumn{
		{Name: "id", DataType: "bigint", UDTName: "int8"},
		{Name: "note", DataType: "text", UDTName: "text", Nullable: true},
		{Name: "flag", DataType: "boolean", UDTName: "bool"},
		{Name: "amount", DataType: "numeric", UDTName: "numeric"},
		{Name: "at", DataType: "timestamp without time zone", UDTName: "timestamp", Nullable: true},
	}}
	if err := copyTable(ctx, source, postgresDB, "public", plan, 2); err != nil {
		t.Fatalf("copyTable: %v", err)
	}

	rows, err := postgresDB.QueryContext(ctx, "SELECT id, note, flag, amount::text, to_char(at, 'YYYY-MM-DD HH24:MI:SS.MS') FROM public."+quotePGIdent(table)+" ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got int
	for rows.Next() {
		var (
			id     int64
			note   sql.NullString
			flag   bool
			amount string
			stamp  sql.NullString
		)
		if err := rows.Scan(&id, &note, &flag, &amount, &stamp); err != nil {
			t.Fatal(err)
		}
		i := int(id) - 1
		if want, _ := notes[i].(string); note.Valid != (notes[i] != nil) || note.String != want {
			t.Errorf("row %d note = %#v, want %#v", id, note, notes[i])
		}
		if flag != (i%2 == 1) {
			t.Errorf("row %d flag = %v", id, flag)
		}
		if want := fmt.Sprintf("%d.%02d", i+10, i); amount != want {
			t.Errorf("row %d amount = %s, want %s", id, amount, want)
		}
		if want := fmt.Sprintf("2026-09-28 1%d:30:00.123", i); (i == 1) == stamp.Valid || (i != 1 && stamp.String != want) {
			t.Errorf("row %d timestamp = %#v, want %q", id, stamp, want)
		}
		got++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got != len(notes) {
		t.Fatalf("copied %d rows, want %d", got, len(notes))
	}
}

// A row PostgreSQL rejects stops COPY while the writer is still streaming
// the rows behind it; the writer then only sees the closed pipe. The
// operator needs PostgreSQL's error, which names the constraint, not the
// pipe's.
func TestCopyTableReportsTheRejectedRow(t *testing.T) {
	postgresDSN := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if postgresDSN == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run the PostgreSQL copy test")
	}
	ctx := context.Background()
	table := fmt.Sprintf("m2p_reject_%d", time.Now().UnixNano())

	source, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer closeDatabase("sqlite", source)
	source.SetMaxOpenConns(1)
	postgresDB, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDatabase("postgres", postgresDB) })

	// Enough rows behind the rejected fifth one that the writer is still
	// streaming when PostgreSQL reports it.
	const rows = 200000
	if _, err := source.ExecContext(ctx, "CREATE TABLE "+quoteMySQLIdent(table)+" (id INTEGER, note TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.ExecContext(ctx, "WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < ?) "+
		"INSERT INTO "+quoteMySQLIdent(table)+" SELECT n, CASE WHEN n = 5 THEN NULL ELSE 'note ' || n END FROM seq", rows); err != nil {
		t.Fatal(err)
	}
	if _, err := postgresDB.ExecContext(ctx, "CREATE TABLE public."+quotePGIdent(table)+" (id bigint, note text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := postgresDB.ExecContext(ctx, "DROP TABLE IF EXISTS public."+quotePGIdent(table)); err != nil {
			t.Error(err)
		}
	})

	plan := tablePlan{Name: table, RowCount: rows, OrderColumns: []string{"id"}, Columns: []postgresColumn{
		{Name: "id", DataType: "bigint", UDTName: "int8"},
		{Name: "note", DataType: "text", UDTName: "text"},
	}}
	err = copyTable(ctx, source, postgresDB, "public", plan, rows)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23502" {
		t.Fatalf("copyTable() = %v, want PostgreSQL's not-null violation (SQLSTATE 23502)", err)
	}
	if errors.Is(err, errCopyStopped) || !strings.Contains(err.Error(), "copy rows into "+table) {
		t.Fatalf("copyTable() = %v, want the table's copy error, not the pipe's", err)
	}
}
