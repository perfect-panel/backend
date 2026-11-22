package schema

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
)

func TestMigrateMySQL(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_MYSQL_DSN to run MySQL/MariaDB migration test")
	}
	runMigration(t, orm.DriverMySQL, dsn)
}

func TestMigratePostgres(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run PostgreSQL migration test")
	}
	runMigration(t, orm.DriverPostgres, dsn)
}

func runMigration(t *testing.T, driver, dsn string) {
	t.Helper()
	err := Up(driver, dsn)
	if err != nil && !errors.Is(err, NoChange) {
		t.Fatalf("%s migration failed: %v", driver, err)
	}
	cfg := orm.ParseDSN(dsn)
	if cfg == nil {
		t.Fatalf("%s dsn parse failed", driver)
	}
	cfg.Driver = orm.NormalizeDriver(driver)
	db, err := orm.ConnectDatabase(orm.Mysql{Config: *cfg})
	if err != nil {
		t.Fatalf("%s connect failed: %v", driver, err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		t.Cleanup(func() {
			if err := sqlDB.Close(); err != nil {
				t.Errorf("%s close failed: %v", driver, err)
			}
		})
	}
	if err := CreateAdminUser(fmt.Sprintf("admin-%s@example.com", driver), "password", db); err != nil {
		t.Fatalf("%s create admin failed: %v", driver, err)
	}
	if err := CreateAdminUser("", "password", db); err != nil {
		t.Fatalf("%s existing admin must skip email validation: %v", driver, err)
	}
}

func TestPostgresMigrationURLUsesThePgxDriver(t *testing.T) {
	cases := map[string]string{
		"postgres://u:p@127.0.0.1:5432/ppanel?sslmode=disable&x-migrations-table=schema_migrations": "pgx5://u:p@127.0.0.1:5432/ppanel?sslmode=disable&x-migrations-table=schema_migrations",
		"postgresql://u@db/ppanel": "pgx5://u@db/ppanel",
		"PostgreSQL://u@db/ppanel": "pgx5://u@db/ppanel",
		"pgx5://u@db/ppanel":       "pgx5://u@db/ppanel",
		"u:p@db:5432/ppanel":       "pgx5://u:p@db:5432/ppanel",
	}
	for dsn, want := range cases {
		if got := postgresMigrationURL(dsn); got != want {
			t.Errorf("postgresMigrationURL(%q) = %q, want %q", dsn, got, want)
		}
	}
}

// The DSN the application builds keeps every parameter, credentials and the
// raw time-zone slash GORM depends on included.
func TestPostgresMigrationURLKeepsTheApplicationDSN(t *testing.T) {
	dsn := orm.Mysql{Config: orm.Config{Driver: orm.DriverPostgres, Addr: "db:5432", Username: "ppanel", Password: "p@ss/word", Dbname: "ppanel"}}.MigrationDsn()

	got := postgresMigrationURL(dsn)

	if want := "pgx5://" + strings.TrimPrefix(dsn, "postgres://"); got != want || !strings.HasPrefix(dsn, "postgres://") {
		t.Fatalf("postgresMigrationURL(%q) = %q, want %q", dsn, got, want)
	}
	for _, part := range []string{"p%40ss%2Fword@db:5432/ppanel?", "sslmode=prefer", "TimeZone=Asia/Shanghai", "application_name=perfect-panel"} {
		if !strings.Contains(got, part) {
			t.Fatalf("migration URL %q lost %q", got, part)
		}
	}
}

// golang-migrate's mysql driver strips the mysql:// scheme, parses the rest
// with the MySQL driver and URL-unescapes the user name and password. A
// password with %, + or @ used to authenticate as something else, so the
// migration failed on a database GORM had just connected to. The test replays
// the driver's sequence on the URL Migrate builds.
func TestMySQLMigrationURLRoundTripsReservedCredentialCharacters(t *testing.T) {
	for _, password := range []string{"Ab+cd9%2F", "p@ss/word", "with space", "q?mark&amp"} {
		m := orm.Mysql{Config: orm.Config{Driver: orm.DriverMySQL, Addr: "db:3306", Username: "us@r", Password: password, Dbname: "ppanel"}}

		databaseURL := ensureScheme(orm.DriverMySQL, m.MigrationDsn())

		if !strings.HasPrefix(databaseURL, "mysql://") || strings.Count(databaseURL, "://") != 1 {
			t.Fatalf("migration URL %q, want exactly one mysql:// scheme", databaseURL)
		}
		cfg, err := mysqldriver.ParseDSN(strings.TrimPrefix(databaseURL, "mysql://"))
		if err != nil {
			t.Fatalf("parse %q: %v", databaseURL, err)
		}
		user, err := url.QueryUnescape(cfg.User)
		if err != nil {
			t.Fatal(err)
		}
		got, err := url.QueryUnescape(cfg.Passwd)
		if err != nil {
			t.Fatal(err)
		}
		if user != "us@r" || got != password || cfg.DBName != "ppanel" || cfg.Addr != "db:3306" {
			t.Fatalf("golang-migrate connects as %q/%q to %s/%s, want us@r/%q to db:3306/ppanel (URL %q)", user, got, cfg.Addr, cfg.DBName, password, databaseURL)
		}
	}
}

// golang-migrate's lib/pq driver registered "postgres"; only the pgx driver,
// which shares GORM's pgx, may be linked now.
func TestOnlyThePgxPostgresMigrationDriverIsLinked(t *testing.T) {
	drivers := database.List()
	if !slices.Contains(drivers, pgxScheme) {
		t.Fatalf("migration drivers %v, want %s registered", drivers, pgxScheme)
	}
	for _, legacy := range []string{"postgres", "postgresql"} {
		if slices.Contains(drivers, legacy) {
			t.Fatalf("migration drivers %v still include the lib/pq driver %q", drivers, legacy)
		}
	}
}

// Up used to panic when it could not open the migration database; startup and
// the setup page now receive the error.
func TestUpReportsDatabasesItCannotOpen(t *testing.T) {
	logtest.Discard(t)
	cases := map[string]string{
		orm.DriverPostgres: "postgres://ppanel:secret@127.0.0.1:1/ppanel?sslmode=disable&connect_timeout=5",
		orm.DriverMySQL:    "ppanel:secret@tcp(127.0.0.1:1)/ppanel?timeout=5s",
		"oracle":           "oracle://ppanel@db/ppanel",
	}
	for driver, dsn := range cases {
		err := Up(driver, dsn)
		if err == nil || errors.Is(err, NoChange) {
			t.Fatalf("Up(%s) = %v, want an error", driver, err)
		}
		if strings.Contains(err.Error(), "unknown driver") {
			t.Fatalf("Up(%s) found no migration driver: %v", driver, err)
		}
	}
}

type fakeMigrationRunner struct {
	upErr       error
	sourceErr   error
	databaseErr error
	closed      bool
}

func (f *fakeMigrationRunner) Up() error { return f.upErr }

func (f *fakeMigrationRunner) Close() (error, error) {
	f.closed = true
	return f.sourceErr, f.databaseErr
}

func TestUpAndCloseAlwaysClosesMigrationRunner(t *testing.T) {
	upFailure := errors.New("up failed")
	tests := []struct {
		name  string
		upErr error
	}{
		{name: "success"},
		{name: "no change", upErr: NoChange},
		{name: "migration failure", upErr: upFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeMigrationRunner{upErr: tt.upErr}
			err := upAndClose(runner)

			if !runner.closed {
				t.Fatal("migration runner was not closed")
			}
			if !errors.Is(err, tt.upErr) {
				t.Fatalf("upAndClose() error = %v, want %v", err, tt.upErr)
			}
		})
	}
}

func TestUpAndCloseDoesNotHideCloseFailureBehindNoChange(t *testing.T) {
	closeFailure := errors.New("database close failed")
	runner := &fakeMigrationRunner{
		upErr:       NoChange,
		databaseErr: closeFailure,
	}

	err := upAndClose(runner)

	if !runner.closed {
		t.Fatal("migration runner was not closed")
	}
	if !errors.Is(err, closeFailure) {
		t.Fatalf("upAndClose() error = %v, want close failure", err)
	}
	if errors.Is(err, NoChange) {
		t.Fatalf("upAndClose() error = %v hides a close failure behind ErrNoChange", err)
	}
}

func TestDialectMigrationVersionsStayAligned(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		mysqlFiles := migrationNames(t, "database/mysql", direction)
		postgresFiles := migrationNames(t, "database/postgres", direction)
		if !reflect.DeepEqual(mysqlFiles, postgresFiles) {
			t.Fatalf("%s migration versions differ:\nmysql:    %v\npostgres: %v", direction, mysqlFiles, postgresFiles)
		}
	}
}

func migrationNames(t *testing.T, directory, direction string) []string {
	t.Helper()
	entries, err := sqlFiles.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s migrations: %v", directory, err)
	}
	suffix := "." + direction + ".sql"
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), suffix)
		version, _, ok := strings.Cut(base, "_")
		if !ok {
			t.Fatalf("migration %s/%s has no version prefix", directory, entry.Name())
		}
		names = append(names, version)
	}
	sort.Strings(names)
	return names
}

func TestPostgresOnlineIndexMigrationsContainOneConcurrentStatement(t *testing.T) {
	entries, err := sqlFiles.ReadDir("database/postgres")
	if err != nil {
		t.Fatalf("read postgres migrations: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name < "02155" || name > "02169_zzzz" {
			continue
		}
		data, err := sqlFiles.ReadFile(filepath.Join("database/postgres", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sql := string(data)
		if !strings.Contains(sql, "CONCURRENTLY") {
			t.Errorf("%s must build or drop its index concurrently", name)
		}
		if got := strings.Count(sql, ";"); got != 1 {
			t.Errorf("%s contains %d statements; online index migrations must contain exactly one", name, got)
		}
	}
}

func TestMySQLPostgresOnlyMigrationsRemainExecutableNoOps(t *testing.T) {
	entries, err := sqlFiles.ReadDir("database/mysql")
	if err != nil {
		t.Fatalf("read mysql migrations: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name < "02155" || name > "02172_zzzz" {
			continue
		}
		data, err := sqlFiles.ReadFile(filepath.Join("database/mysql", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(data), "SELECT 1;") {
			t.Errorf("%s must be an executable no-op, not an empty/comment-only query", name)
		}
	}
}

func TestPostgresMySQLOnlyMigrationsRemainExecutableNoOps(t *testing.T) {
	entries, err := sqlFiles.ReadDir("database/postgres")
	if err != nil {
		t.Fatalf("read postgres migrations: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name < "02173" || name > "02178_zzzz" {
			continue
		}
		data, err := sqlFiles.ReadFile(filepath.Join("database/postgres", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(data), "SELECT 1;") {
			t.Errorf("%s must be an executable no-op, not an empty/comment-only query", name)
		}
	}
}

func TestMySQLTaskScopeGeneratedColumnUsesInstantDDL(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		name := "02177_mysql_task_scope_generated." + direction + ".sql"
		data, err := sqlFiles.ReadFile(filepath.Join("database/mysql", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sql := string(data)
		if !strings.Contains(sql, "ALGORITHM=INSTANT") {
			t.Errorf("%s must add or drop the virtual column without rebuilding the table", name)
		}
		if strings.Contains(sql, "LOCK=") {
			t.Errorf("%s must not combine ALGORITHM=INSTANT with a LOCK clause", name)
		}
		if got := strings.Count(sql, ";"); got != 1 {
			t.Errorf("%s contains %d statements, want 1", name, got)
		}
	}
}

func TestMySQLTaskScopeIndexUsesPortableOnlineDDL(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		name := "02178_mysql_task_scope_index." + direction + ".sql"
		data, err := sqlFiles.ReadFile(filepath.Join("database/mysql", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sql := string(data)
		// MySQL 8 can build this index INPLACE. MariaDB must choose its
		// online COPY path once a virtual generated column becomes indexed.
		if !strings.Contains(sql, "ALGORITHM=DEFAULT") || !strings.Contains(sql, "LOCK=NONE") {
			t.Errorf("%s must let each engine choose its compatible online algorithm", name)
		}
		if direction == "up" && (!strings.Contains(sql, "`created_at` DESC") || !strings.Contains(sql, "`id` DESC")) {
			t.Errorf("%s must match newest-first task pagination", name)
		}
		if got := strings.Count(sql, ";"); got != 1 {
			t.Errorf("%s contains %d statements, want 1", name, got)
		}
	}
}

func TestMySQLHotIndexMigrationsUseOnlineDDL(t *testing.T) {
	entries, err := sqlFiles.ReadDir("database/mysql")
	if err != nil {
		t.Fatalf("read mysql migrations: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name < "02173" || name > "02176_zzzz" {
			continue
		}
		data, err := sqlFiles.ReadFile(filepath.Join("database/mysql", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sql := string(data)
		if !strings.Contains(sql, "ALGORITHM=INPLACE") || !strings.Contains(sql, "LOCK=NONE") {
			t.Errorf("%s must request online InnoDB DDL", name)
		}
		if got := strings.Count(sql, ";"); got != 1 {
			t.Errorf("%s contains %d statements; each table optimization must be atomic", name, got)
		}
	}
}
