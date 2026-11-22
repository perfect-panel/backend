package orm

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseMySQLDSN(t *testing.T) {
	cfg := ParseDSN("root:password@tcp(localhost:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai")
	if cfg == nil {
		t.Fatal("config is nil")
	}
	if cfg.Driver != DriverMySQL || cfg.Addr != "localhost:3306" || cfg.Dbname != "ppanel" || cfg.Username != "root" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	query, err := mysqlDSNQuery(Mysql{Config: *cfg}.Dsn())
	if err != nil {
		t.Fatalf("parse generated mysql dsn: %v", err)
	}
	if got := query.Get("interpolateParams"); got != "true" {
		t.Fatalf("mysql interpolateParams = %q, want true", got)
	}
}

func TestMySQLDSNPreservesInterpolationOverrideAndUnsafeCharset(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{name: "explicit false", config: legacyDefaultMySQLConfig + "&interpolateParams=false", want: "false"},
		{name: "legacy utf8 default", config: legacyDefaultMySQLConfig, want: "true"},
		{name: "custom legacy charset", config: "charset=gbk&parseTime=true", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := mysqlDSNQuery((Mysql{Config: Config{
				Driver: DriverMySQL, Addr: "localhost:3306", Dbname: "ppanel",
				Username: "root", Password: "password", Config: tt.config,
			}}).Dsn())
			if err != nil {
				t.Fatalf("parse generated mysql dsn: %v", err)
			}
			if got := query.Get("interpolateParams"); got != tt.want {
				t.Fatalf("interpolateParams = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseMySQLDSNPreservesDriverOptions(t *testing.T) {
	cfg := ParseDSN("root:password@tcp(localhost:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=false&readTimeout=3s")
	if cfg == nil {
		t.Fatal("config is nil")
	}
	query, err := mysqlDSNQuery(Mysql{Config: *cfg}.Dsn())
	if err != nil {
		t.Fatalf("parse generated mysql dsn: %v", err)
	}
	if got := query.Get("interpolateParams"); got != "false" {
		t.Fatalf("interpolateParams = %q, want false", got)
	}
	if got := query.Get("readTimeout"); got != "3s" {
		t.Fatalf("readTimeout = %q, want 3s", got)
	}
}

func TestParseMySQLDSNDoesNotTreatPasswordQuestionMarkAsOptions(t *testing.T) {
	cfg := ParseDSN("root:secret?interpolateParams=false@tcp(localhost:3306)/ppanel")
	if cfg == nil {
		t.Fatal("config is nil")
	}
	if cfg.Password != "secret?interpolateParams=false" {
		t.Fatalf("password = %q, want embedded question mark preserved", cfg.Password)
	}
	query, err := mysqlDSNQuery(Mysql{Config: *cfg}.Dsn())
	if err != nil {
		t.Fatalf("parse generated mysql dsn: %v", err)
	}
	if got := query.Get("interpolateParams"); got != "true" {
		t.Fatalf("interpolateParams = %q, want safe default true", got)
	}
}

func TestParseMySQLURLDSNAppliesOnlySafeInterpolationDefault(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "utf8mb4",
			dsn:  "mysql://root:password@localhost:3306/ppanel?charset=utf8mb4&parseTime=true",
			want: "true",
		},
		{
			name: "explicit false",
			dsn:  "mysql://root:password@localhost:3306/ppanel?charset=utf8mb4&interpolateParams=false",
			want: "false",
		},
		{
			name: "legacy charset",
			dsn:  "mysql://root:password@localhost:3306/ppanel?charset=gbk",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ParseDSN(tt.dsn)
			if cfg == nil {
				t.Fatal("config is nil")
			}
			query, err := url.ParseQuery(cfg.Config)
			if err != nil {
				t.Fatalf("parse config query: %v", err)
			}
			if got := query.Get("interpolateParams"); got != tt.want {
				t.Fatalf("interpolateParams = %q, want %q", got, tt.want)
			}
		})
	}
}

func mysqlDSNQuery(dsn string) (url.Values, error) {
	rawQuery := MySQLDSNRawQuery(dsn)
	if rawQuery == "" {
		return nil, nil
	}
	return url.ParseQuery(rawQuery)
}

func TestParsePostgresDSN(t *testing.T) {
	cfg := ParseDSN("postgres://postgres:password@localhost:5432/ppanel?sslmode=disable&TimeZone=Asia%2FShanghai")
	if cfg == nil {
		t.Fatal("config is nil")
	}
	if cfg.Driver != DriverPostgres || cfg.Addr != "localhost:5432" || cfg.Dbname != "ppanel" || cfg.Username != "postgres" {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	dsn := Mysql{Config: *cfg}.Dsn()
	if !strings.Contains(dsn, "TimeZone=Asia/Shanghai") {
		t.Fatalf("postgres dsn %q must expose the IANA zone to GORM", dsn)
	}
	if strings.Contains(dsn, "TimeZone=Asia%2FShanghai") {
		t.Fatalf("postgres dsn %q contains a URL-encoded zone that GORM treats literally", dsn)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse generated postgres dsn: %v", err)
	}
	if got := parsed.Query().Get("TimeZone"); got != "Asia/Shanghai" {
		t.Fatalf("postgres TimeZone = %q, want Asia/Shanghai", got)
	}
	if got := parsed.Query().Get("application_name"); got != defaultPostgresApplicationName {
		t.Fatalf("postgres application_name = %q, want %q", got, defaultPostgresApplicationName)
	}
	if cfg.ConnMaxLifetime != DefaultConnMaxLifetimeSeconds || cfg.ConnMaxIdleTime != DefaultConnMaxIdleTimeSeconds {
		t.Fatalf("postgres pool lifetimes = (%d, %d), want (%d, %d)",
			cfg.ConnMaxLifetime, cfg.ConnMaxIdleTime, DefaultConnMaxLifetimeSeconds, DefaultConnMaxIdleTimeSeconds)
	}
}

func TestPostgresDSNAcceptsLiteralAndEncodedIANAZone(t *testing.T) {
	for _, zone := range []string{"Asia/Shanghai", "Asia%2FShanghai"} {
		t.Run(zone, func(t *testing.T) {
			cfg := Config{
				Driver: DriverPostgres, Addr: "localhost:5432", Dbname: "ppanel",
				Username: "postgres", Password: "password", Config: "sslmode=disable&TimeZone=" + zone,
			}
			dsn := (Mysql{Config: cfg}).Dsn()
			if !strings.Contains(dsn, "TimeZone=Asia/Shanghai") {
				t.Fatalf("postgres dsn %q must expose the IANA zone to GORM", dsn)
			}
		})
	}
}

func TestPostgresDSNPreservesApplicationNameOverride(t *testing.T) {
	cfg := Config{
		Driver:   DriverPostgres,
		Addr:     "localhost:5432",
		Dbname:   "ppanel",
		Username: "postgres",
		Config:   "sslmode=disable&application_name=worker",
	}
	parsed, err := url.Parse((Mysql{Config: cfg}).Dsn())
	if err != nil {
		t.Fatalf("parse generated postgres dsn: %v", err)
	}
	if got := parsed.Query().Get("application_name"); got != "worker" {
		t.Fatalf("postgres application_name = %q, want worker", got)
	}
}

// The default parameters keep Asia/Shanghai unless a location is given.
func TestDefaultParametersFollowTheLocation(t *testing.T) {
	if DefaultMySQLQuery("") != DefaultMySQLConfig || DefaultMySQLQuery(DefaultLocation) != DefaultMySQLConfig {
		t.Fatalf("default MySQL parameters changed: %q", DefaultMySQLQuery(""))
	}
	const defaultPostgresQuery = "sslmode=prefer&TimeZone=Asia/Shanghai&application_name=perfect-panel"
	if DefaultPostgresQuery("") != defaultPostgresQuery || DefaultPostgresQuery(DefaultLocation) != defaultPostgresQuery {
		t.Fatalf("default PostgreSQL parameters changed: %q", DefaultPostgresQuery(""))
	}
	if legacyMySQLQuery("") != legacyDefaultMySQLConfig {
		t.Fatalf("legacy MySQL parameters changed: %q", legacyMySQLQuery(""))
	}

	mysqlDSN := Mysql{Config: Config{Addr: "db:3306", Dbname: "ppanel", Username: "u", Password: "p"}, Location: "Europe/Berlin"}.Dsn()
	query, err := mysqlDSNQuery(mysqlDSN)
	if err != nil || query.Get("loc") != "Europe/Berlin" || query.Get("parseTime") != "true" {
		t.Fatalf("MySQL DSN %q (err %v), want loc=Europe/Berlin", mysqlDSN, err)
	}

	// A zone whose name needs escaping still reaches PostgreSQL intact.
	for _, stored := range []string{"", DefaultMySQLConfig, DefaultMySQLQuery("Etc/GMT+8")} {
		dsn := Mysql{Config: Config{Driver: DriverPostgres, Addr: "db:5432", Dbname: "ppanel", Config: stored}, Location: "Etc/GMT+8"}.Dsn()
		parsed, err := url.Parse(dsn)
		if err != nil || parsed.Query().Get("TimeZone") != "Etc/GMT+8" || !strings.Contains(dsn, "TimeZone=Etc/GMT%2B8") {
			t.Fatalf("stored %q: PostgreSQL DSN %q (err %v), want TimeZone Etc/GMT+8 with a visible slash", stored, dsn, err)
		}
	}
}

func TestParseDSNInAppliesTheLocationToBareDSNs(t *testing.T) {
	for dsn, key := range map[string]string{
		"root:pw@tcp(db:3306)/ppanel":         "loc",
		"mysql://root:pw@db:3306/ppanel":      "loc",
		"postgres://root:pw@db:5432/ppanel":   "TimeZone",
		"postgresql://root:pw@db:5432/ppanel": "TimeZone",
	} {
		cfg := ParseDSNIn(dsn, "UTC")
		if cfg == nil {
			t.Fatalf("%s: not parsed", dsn)
		}
		params, err := url.ParseQuery(cfg.Config)
		if err != nil || params.Get(key) != "UTC" {
			t.Fatalf("%s: parameters %q (err %v), want %s=UTC", dsn, cfg.Config, err, key)
		}
		if def := ParseDSN(dsn); def == nil || !strings.Contains(def.Config, "Shanghai") {
			t.Fatalf("%s: ParseDSN parameters %+v, want the Asia/Shanghai default", dsn, def)
		}
	}
	// Explicit parameters win over the location.
	if cfg := ParseDSNIn("root:pw@tcp(db:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FTokyo", "UTC"); cfg == nil || !strings.Contains(cfg.Config, "Tokyo") {
		t.Fatalf("explicit loc overridden: %+v", cfg)
	}
}

func pingDatabase(t *testing.T, dsn string) {
	t.Helper()
	cfg := ParseDSN(dsn)
	if cfg == nil {
		t.Fatalf("parse test DSN %q", dsn)
	}
	db, err := ConnectDatabase(Mysql{Config: *cfg})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("connection pool: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestPingMySQL(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_MYSQL_DSN to run MySQL/MariaDB ping test")
	}
	pingDatabase(t, dsn)
}

func TestPingPostgres(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run PostgreSQL ping test")
	}
	pingDatabase(t, dsn)
}

func TestConnectPostgresWithIANAZone(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run PostgreSQL connection tests")
	}
	cfg := ParseDSN(dsn)
	if cfg == nil {
		t.Fatalf("parse PostgreSQL test DSN %q", dsn)
	}
	params, err := url.ParseQuery(cfg.Config)
	if err != nil {
		t.Fatalf("parse PostgreSQL test parameters: %v", err)
	}
	params.Set("TimeZone", "Asia/Shanghai")
	cfg.Config = params.Encode()

	db, err := ConnectDatabase(Mysql{Config: *cfg})
	if err != nil {
		t.Fatalf("connect PostgreSQL with IANA zone: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get PostgreSQL connection pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var zone string
	if err := sqlDB.QueryRowContext(ctx, "SHOW timezone").Scan(&zone); err != nil {
		t.Fatalf("read PostgreSQL session timezone: %v", err)
	}
	if zone != "Asia/Shanghai" {
		t.Fatalf("PostgreSQL session timezone = %q, want Asia/Shanghai", zone)
	}
}
