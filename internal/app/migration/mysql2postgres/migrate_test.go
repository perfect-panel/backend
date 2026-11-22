package mysql2postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestQuoteIdentifiers(t *testing.T) {
	if got := quoteMySQLIdent("or`der"); got != "`or``der`" {
		t.Fatalf("quoteMySQLIdent() = %q", got)
	}
	if got := quotePGIdent(`or"der`); got != `"or""der"` {
		t.Fatalf("quotePGIdent() = %q", got)
	}
}

func normalizedSourceDSN(t *testing.T, dsn string, location *time.Location) string {
	t.Helper()
	got, err := normalizeMySQLDSN(dsn, location)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNormalizeMySQLDSN(t *testing.T) {
	got := normalizedSourceDSN(t, "user:pass@tcp(127.0.0.1:3306)/ppanel", time.UTC)
	if got == "user:pass@tcp(127.0.0.1:3306)/ppanel" {
		t.Fatalf("normalizeMySQLDSN() did not add params")
	}
	if !containsAll(got, []string{"parseTime=true", "charset=utf8mb4"}) {
		t.Fatalf("normalizeMySQLDSN() = %q, want parseTime and charset", got)
	}
}

// The driver stores charset separately from its generic Params map.
func TestNormalizeMySQLDSNPreservesCharset(t *testing.T) {
	for _, charset := range []string{"latin1", "utf8mb4,utf8"} {
		dsn := "reader:p?charset=other@tcp(localhost:3306)/ppanel?charset=" + charset
		got := normalizedSourceDSN(t, dsn, time.UTC)
		if strings.Count(got[strings.LastIndex(got, "/")+1:], "charset=") != 1 || !strings.Contains(got, "charset="+charset) {
			t.Errorf("charset %q was overwritten or duplicated: %q", charset, got)
		}
	}
}

// The source DSN reads DATETIME values in the configured zone unless it
// names loc itself; the driver's UTC default labelled every value wrong and
// shifted the timestamptz target columns by the panel's offset.
func TestNormalizeMySQLDSNReadsTimesInTheConfiguredZone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		dsn  string
		want string
	}{
		"no parameters":     {"user:pass@tcp(127.0.0.1:3306)/ppanel", "Europe/Paris"},
		"other parameters":  {"user:pass@tcp(127.0.0.1:3306)/ppanel?charset=utf8mb4&parseTime=true", "Europe/Paris"},
		"explicit loc":      {"user:pass@tcp(127.0.0.1:3306)/ppanel?loc=Asia%2FTokyo", "Asia/Tokyo"},
		"explicit UTC":      {"user:pass@tcp(127.0.0.1:3306)/ppanel?loc=UTC", "UTC"},
		"slash in password": {"user:p/ss?loc=x@tcp(127.0.0.1:3306)/ppanel", "Europe/Paris"},
		"explicit local":    {"user:pass@tcp(127.0.0.1:3306)/ppanel?parseTime=true&loc=Local", time.Local.String()},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := mysqlDriver.ParseDSN(normalizedSourceDSN(t, tc.dsn, paris))
			if err != nil {
				t.Fatalf("normalized DSN does not parse: %v", err)
			}
			if !cfg.ParseTime || cfg.Loc == nil || cfg.Loc.String() != tc.want {
				t.Fatalf("normalizeMySQLDSN(%q) reads times in %v (parseTime %t), want %s", tc.dsn, cfg.Loc, cfg.ParseTime, tc.want)
			}
		})
	}
}

// Migrate refuses a zone it cannot load rather than copying with the wrong
// one.
func TestMigrateRefusesAnUnknownLocation(t *testing.T) {
	for _, location := range []string{"", "Local", "Mars/Olympus"} {
		cfg := DefaultConfig()
		cfg.MySQLDSN, cfg.PostgresDSN, cfg.Location = "u:p@tcp(127.0.0.1:1)/x", "postgres://u:p@127.0.0.1:1/x", location
		err := Migrate(context.Background(), cfg)
		if err == nil || !strings.Contains(err.Error(), "--location") {
			t.Fatalf("Migrate() with location %q = %v, want the location refused", location, err)
		}
	}
	if DefaultConfig().Location != "Asia/Shanghai" {
		t.Fatalf("default location = %q, want the application's default zone", DefaultConfig().Location)
	}
	cfg, err := ParseFlags([]string{"--mysql", "a", "--postgres", "b", "--location", "Europe/Paris"})
	if err != nil || cfg.Location != "Europe/Paris" {
		t.Fatalf("ParseFlags location = %q (%v), want Europe/Paris", cfg.Location, err)
	}
}

func TestConvertValueBoolean(t *testing.T) {
	col := postgresColumn{DataType: "boolean", UDTName: "bool"}
	tests := []struct {
		name  string
		input any
		want  bool
	}{
		{name: "int64 true", input: int64(1), want: true},
		{name: "int64 false", input: int64(0), want: false},
		{name: "bytes true", input: []byte("1"), want: true},
		{name: "bytes false", input: []byte("false"), want: false},
		{name: "string true", input: "true", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertValue(tt.input, col, time.UTC)
			if err != nil {
				t.Fatalf("convertValue() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("convertValue() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestConvertValueInteger(t *testing.T) {
	col := postgresColumn{DataType: "bigint", UDTName: "int8"}
	got, err := convertValue([]byte("42"), col, time.UTC)
	if err != nil {
		t.Fatalf("convertValue() error = %v", err)
	}
	if got != int64(42) {
		t.Fatalf("convertValue() = %#v, want int64(42)", got)
	}
}

func TestConvertValueTimestamp(t *testing.T) {
	col := postgresColumn{DataType: "timestamp without time zone", UDTName: "timestamp"}
	got, err := convertValue([]byte("2026-05-21 14:30:00"), col, time.UTC)
	if err != nil {
		t.Fatalf("convertValue() error = %v", err)
	}
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("convertValue() = %T, want time.Time", got)
	}

	got, err = convertValue([]byte("0000-00-00 00:00:00"), col, time.UTC)
	if err != nil {
		t.Fatalf("convertValue() zero date error = %v", err)
	}
	if got != nil {
		t.Fatalf("convertValue() zero date = %#v, want nil", got)
	}
}

func TestConvertValueKeepsZeroDatePrefixInText(t *testing.T) {
	const note = "0000-00-00 was the original date"
	for _, raw := range []any{note, []byte(note)} {
		got, err := convertValue(raw, postgresColumn{DataType: "text"}, time.UTC)
		if got != note || err != nil {
			t.Fatalf("copied text = %v, %v", got, err)
		}
	}
}

// A timestamp that arrives as text is a wall clock in the configured zone,
// not in the zone of the machine the tool runs on, and COPY receives it
// with that offset so a timestamptz column stores the right instant.
func TestTimestampsAreParsedInTheConfiguredZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })

	col := postgresColumn{DataType: "timestamp with time zone", UDTName: "timestamptz"}
	got, err := convertValue([]byte("2026-09-28 20:00:00"), col, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := got.(time.Time)
	if !ok {
		t.Fatalf("convertValue() = %T, want time.Time", got)
	}
	want := time.Date(2026, 9, 28, 20, 0, 0, 0, shanghai)
	if !parsed.Equal(want) || parsed.Location().String() != "Asia/Shanghai" {
		t.Fatalf("parsed %s, want %s in Asia/Shanghai", parsed, want)
	}
	if text := copyText(parsed); text != "2026-09-28 20:00:00+08:00" {
		t.Fatalf("copyText = %q, want the wall clock with its offset", text)
	}
	// An offset in the value wins over the configured zone.
	if got, err := parseTimestamp("2026-09-28T20:00:00Z", shanghai); err != nil || !got.Equal(time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseTimestamp(RFC3339) = %s (%v), want the value's own zone", got, err)
	}
	// A date is a midnight in the configured zone.
	if got, err := parseTimestamp("2026-09-28", shanghai); err != nil || !got.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, shanghai)) {
		t.Fatalf("parseTimestamp(date) = %s (%v), want midnight in Asia/Shanghai", got, err)
	}
	if _, err := parseTimestamp("yesterday", shanghai); err == nil {
		t.Fatal("parseTimestamp accepted a value no layout parses")
	}
}

func TestBuildPlansSkipsGeneratedColumns(t *testing.T) {
	cols := []postgresColumn{
		{Name: "id", DataType: "bigint"},
		{Name: "computed", DataType: "text", Generated: true},
		{Name: "missing", DataType: "text"},
		{Name: "name", DataType: "text"},
	}
	source := map[string]struct{}{
		"id":       {},
		"computed": {},
		"name":     {},
	}
	common := make([]postgresColumn, 0, len(cols))
	for _, col := range cols {
		if col.Generated {
			continue
		}
		if _, ok := source[col.Name]; ok {
			common = append(common, col)
		}
	}
	if len(common) != 2 || common[0].Name != "id" || common[1].Name != "name" {
		t.Fatalf("common columns = %#v", common)
	}
}

func TestParseTableSet(t *testing.T) {
	got := parseTableSet(" user, order ,,payment ")
	for _, name := range []string{"user", "order", "payment"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("missing table %q in %#v", name, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("parseTableSet length = %d, want 3", len(got))
	}
}

func TestSortPlansByDependencies(t *testing.T) {
	plans := []tablePlan{
		{Name: "order"},
		{Name: "user_subscribe"},
		{Name: "user"},
		{Name: "subscribe"},
	}
	dependencies := []foreignKey{
		{ChildTable: "order", ParentTable: "user"},
		{ChildTable: "order", ParentTable: "subscribe"},
		{ChildTable: "user_subscribe", ParentTable: "user"},
		{ChildTable: "user_subscribe", ParentTable: "subscribe"},
	}
	got := sortPlansByDependencies(plans, dependencies)

	positions := make(map[string]int, len(got))
	for i, plan := range got {
		positions[plan.Name] = i
	}
	for _, dep := range dependencies {
		if positions[dep.ParentTable] > positions[dep.ChildTable] {
			t.Fatalf("%s should be copied before %s; got %#v", dep.ParentTable, dep.ChildTable, got)
		}
	}
}

func TestSortPlansByDependenciesIgnoresSelfReferences(t *testing.T) {
	got := sortPlansByDependencies(
		[]tablePlan{{Name: "order"}, {Name: "user"}},
		[]foreignKey{
			{ChildTable: "order", ParentTable: "order"},
			{ChildTable: "order", ParentTable: "user"},
		},
	)
	if len(got) != 2 || got[0].Name != "user" || got[1].Name != "order" {
		t.Fatalf("sortPlansByDependencies() = %#v", got)
	}
}

func TestIsBoolColumn(t *testing.T) {
	if !isBoolColumn(postgresColumn{DataType: "boolean"}) {
		t.Fatal("boolean data type should be bool")
	}
	if !isBoolColumn(postgresColumn{UDTName: "bool"}) {
		t.Fatal("bool udt should be bool")
	}
	if isBoolColumn(postgresColumn{DataType: "smallint"}) {
		t.Fatal("smallint should not be bool")
	}
}

func TestNullableNullIsPreserved(t *testing.T) {
	got, err := convertValue(nil, postgresColumn{DataType: "text", Nullable: true, Default: sql.NullString{}}, time.UTC)
	if err != nil {
		t.Fatalf("convertValue() error = %v", err)
	}
	if got != nil {
		t.Fatalf("convertValue(nil) = %#v, want nil", got)
	}
}

func containsAll(value string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
