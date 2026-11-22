// Package orm opens the application's database, MySQL or PostgreSQL, from
// the database configuration, and holds the dialect-aware query helpers
// (CSV-column filters, escaped LIKE searches, date buckets, batched deletes)
// that repositories need to run the same queries on both drivers. The
// default connection parameters pin the session time zone, which stored
// times and per-day statistics are in.
package orm

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/perfect-panel/backend/pkg/logger"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const (
	// DriverMySQL and DriverPostgres are the drivers NormalizeDriver
	// returns; DriverPostgres2 is the other spelling it accepts for
	// PostgreSQL, the scheme of postgresql:// DSNs.
	DriverMySQL     = "mysql"
	DriverPostgres  = "postgres"
	DriverPostgres2 = "postgresql"

	// DefaultLocation is the time zone the default connection parameters
	// use when the caller names none.
	DefaultLocation = "Asia/Shanghai"
	// DefaultMySQLConfig is the default MySQL connection parameters for
	// DefaultLocation, the value DefaultMySQLQuery returns for it.
	DefaultMySQLConfig       = "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=true"
	legacyDefaultMySQLConfig = "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"
	// DefaultSlowThresholdMs, DefaultConnMaxLifetimeSeconds and
	// DefaultConnMaxIdleTimeSeconds are the slow-query and pool settings of
	// a configuration built in code, the same as Config's default tags.
	DefaultSlowThresholdMs         = 1000
	DefaultConnMaxLifetimeSeconds  = 1800
	DefaultConnMaxIdleTimeSeconds  = 300
	defaultPostgresApplicationName = "perfect-panel"
)

// locationOrDefault returns location when both drivers can use it: an IANA
// zone name. Empty, "Local" (which PostgreSQL does not know) and names the
// time package cannot load fall back to DefaultLocation.
func locationOrDefault(location string) string {
	if location == "" || location == "Local" {
		return DefaultLocation
	}
	if _, err := time.LoadLocation(location); err != nil {
		return DefaultLocation
	}
	return location
}

// postgresTimeZoneKeys are the spellings of the PostgreSQL session time-zone
// parameter the DSN may use; GORM's dialector matches all three.
var postgresTimeZoneKeys = []string{"TimeZone", "timezone", "time_zone"}

// postgresTimeZone returns the session zone params name, or "" when they
// name none.
func postgresTimeZone(params url.Values) string {
	for _, key := range postgresTimeZoneKeys {
		if zone := params.Get(key); zone != "" {
			return zone
		}
	}
	return ""
}

// SessionLocation reports the time zone the connection stores and reads
// timestamps in: the loc parameter for MySQL (the driver's UTC when custom
// parameters name none, the process zone's name for "Local"), the TimeZone
// parameter for PostgreSQL (Location's zone when custom parameters name
// none, since postgresDsn adds it), and the default parameters' zone
// otherwise. It is "" only for parameters that do not parse. Stored times
// and per-day statistics are in this zone.
func (m Mysql) SessionLocation() string {
	params := m.Config.Config
	if m.Driver() == DriverPostgres {
		if params == "" || isDefaultMySQLQuery(params, m.Location) {
			return locationOrDefault(m.Location)
		}
		values, err := url.ParseQuery(params)
		if err != nil {
			return ""
		}
		if zone := postgresTimeZone(values); zone != "" {
			return zone
		}
		return locationOrDefault(m.Location)
	}
	if params == "" {
		return locationOrDefault(m.Location)
	}
	values, err := url.ParseQuery(params)
	if err != nil {
		return ""
	}
	switch zone := values.Get("loc"); zone {
	case "":
		return "UTC"
	case "Local":
		// The driver reads "Local" as time.Local, which the server sets to
		// AppLocation at startup; the zone's own name is what the startup
		// comparison with AppLocation expects.
		return time.Local.String()
	default:
		return zone
	}
}

// DefaultMySQLQuery returns the default MySQL connection parameters, reading
// DATETIME values in location (DefaultLocation when empty).
func DefaultMySQLQuery(location string) string {
	return legacyMySQLQuery(location) + "&interpolateParams=true"
}

// legacyMySQLQuery is the default MySQL parameters before interpolation was
// enabled by default.
func legacyMySQLQuery(location string) string {
	return "charset=utf8mb4&parseTime=true&loc=" + url.QueryEscape(locationOrDefault(location))
}

// DefaultPostgresSSLMode is the sslmode of the default PostgreSQL
// parameters: the connection is encrypted when the server offers TLS and
// falls back to plaintext when it does not, so a server without a
// certificate still works. It does not verify the server; deployments that
// reach the database over a network should set sslmode=verify-full in the
// parameters. Configurations written before this default keep their
// sslmode=disable.
const DefaultPostgresSSLMode = "prefer"

// DefaultPostgresQuery returns the default PostgreSQL connection parameters,
// with the session time zone set to location (DefaultLocation when empty).
// The zone's slashes stay visible; see encodePostgresParams.
func DefaultPostgresQuery(location string) string {
	zone := strings.ReplaceAll(url.QueryEscape(locationOrDefault(location)), "%2F", "/")
	return "sslmode=" + DefaultPostgresSSLMode + "&TimeZone=" + zone + "&application_name=" + defaultPostgresApplicationName
}

// isDefaultMySQLQuery reports whether query is a default MySQL parameter set
// written for location or for DefaultLocation, which a PostgreSQL
// connection replaces with its own defaults.
func isDefaultMySQLQuery(query, location string) bool {
	switch query {
	case DefaultMySQLConfig, legacyDefaultMySQLConfig, DefaultMySQLQuery(location), legacyMySQLQuery(location):
		return true
	}
	return false
}

// Config is the database configuration (the Database section).
type Config struct {
	Driver          string `yaml:"Driver" default:"mysql"`
	Addr            string `yaml:"Addr"`
	Username        string `yaml:"Username"`
	Password        string `yaml:"Password"`
	Dbname          string `yaml:"Dbname"`
	Config          string `yaml:"Config" default:"charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=true"`
	MaxIdleConns    int    `yaml:"MaxIdleConns" default:"10"`
	MaxOpenConns    int    `yaml:"MaxOpenConns" default:"10"`
	ConnMaxLifetime int64  `yaml:"ConnMaxLifetime" default:"1800"`
	ConnMaxIdleTime int64  `yaml:"ConnMaxIdleTime" default:"300"`
	SlowThreshold   int64  `yaml:"SlowThreshold" default:"1000"`
}

// Mysql is a database connection to open, of either driver despite its
// name.
type Mysql struct {
	Config Config
	// Location is the IANA time zone of the default connection parameters,
	// used when Config.Config is empty or, for PostgreSQL, still a MySQL
	// default, and the session zone of PostgreSQL parameters that name none.
	// Empty keeps DefaultLocation.
	Location string
}

// NormalizeDriver maps the driver names a configuration or DSN may use onto
// DriverMySQL (the default, for an empty name) or DriverPostgres; an unknown
// name is returned lowercased, for the caller to reject.
func NormalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", DriverMySQL:
		return DriverMySQL
	case DriverPostgres, DriverPostgres2, "pgsql":
		return DriverPostgres
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

// Driver returns the connection's normalized driver.
func (m Mysql) Driver() string {
	return NormalizeDriver(m.Config.Driver)
}

// Dsn returns the DSN the driver connects with, with the default
// parameters filled in.
func (m Mysql) Dsn() string {
	switch m.Driver() {
	case DriverPostgres:
		return m.postgresDsn()
	default:
		return m.mysqlDsn()
	}
}

// MigrationDsn returns the DSN the schema migrations connect with. For
// MySQL the user name and password are URL-escaped: golang-migrate's mysql
// driver unescapes both (a compatibility remnant of when it parsed the DSN
// with net/url), so a password with %, + or @ handed over as is would be
// altered. The PostgreSQL DSN is a URL already, with escaped credentials.
func (m Mysql) MigrationDsn() string {
	if m.Driver() != DriverMySQL {
		return m.Dsn()
	}
	return url.QueryEscape(m.Config.Username) + ":" + url.QueryEscape(m.Config.Password) + "@tcp(" + m.Config.Addr + ")/" + m.Config.Dbname + "?" + m.mysqlQuery()
}

func (m Mysql) mysqlDsn() string {
	return m.Config.Username + ":" + m.Config.Password + "@tcp(" + m.Config.Addr + ")/" + m.Config.Dbname + "?" + m.mysqlQuery()
}

// mysqlQuery returns the MySQL connection parameters with the defaults
// filled in.
func (m Mysql) mysqlQuery() string {
	query := m.Config.Config
	if query == "" {
		return DefaultMySQLQuery(m.Location)
	}
	return withDefaultMySQLParams(query)
}

// withDefaultMySQLParams enables client-side placeholder interpolation only
// for the UTF-8 connection settings the application supports by default. It
// saves a prepare/execute/close round trip for ordinary GORM queries, while an
// explicit false value and custom legacy multibyte character sets are left
// untouched.
func withDefaultMySQLParams(query string) string {
	params, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	if _, configured := params["interpolateParams"]; configured {
		return query
	}
	charset := strings.ToLower(params.Get("charset"))
	collation := strings.ToLower(params.Get("collation"))
	if charset != "utf8mb4" && charset != "utf8mb4,utf8" {
		return query
	}
	if collation != "" && !strings.HasPrefix(collation, "utf8mb4_") && !strings.HasPrefix(collation, "utf8_") {
		return query
	}
	params.Set("interpolateParams", "true")
	return params.Encode()
}

func (m Mysql) postgresDsn() string {
	query := m.Config.Config
	if query == "" || isDefaultMySQLQuery(query, m.Location) {
		query = DefaultPostgresQuery(m.Location)
	}
	u := url.URL{
		Scheme: DriverPostgres,
		Host:   m.Config.Addr,
		Path:   "/" + m.Config.Dbname,
	}
	if m.Config.Username != "" {
		u.User = url.UserPassword(m.Config.Username, m.Config.Password)
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		u.RawQuery = query
	} else {
		if params.Get("application_name") == "" {
			params.Set("application_name", defaultPostgresApplicationName)
		}
		// GORM registers the timestamp codec that reads timestamp columns
		// in the DSN's zone only when the DSN names one; without it every
		// stored time reads back labelled UTC, hours off the zone it was
		// written in. Custom parameters that leave the zone out get
		// Location's zone, like the default parameters.
		if postgresTimeZone(params) == "" {
			params.Set("TimeZone", locationOrDefault(m.Location))
		}
		u.RawQuery = encodePostgresParams(params)
	}
	return u.String()
}

// encodePostgresParams keeps IANA time-zone separators visible in the final
// DSN. pgx decodes ordinary URL query values correctly, but GORM's PostgreSQL
// dialector also extracts TimeZone directly from the raw DSN with a regular
// expression. Leaving the slash as %2F therefore makes GORM ask both Go and
// PostgreSQL for a literal zone such as "Asia%2FShanghai".
//
// Only the slash in supported time-zone parameters is unescaped; all other
// parameter values remain URL encoded. This preserves credentials and custom
// settings while accepting both legacy Asia%2FShanghai configuration and the
// clearer Asia/Shanghai form.
func encodePostgresParams(params url.Values) string {
	query := params.Encode()
	for _, key := range []string{"TimeZone", "timezone", "time_zone"} {
		for _, value := range params[key] {
			encodedKey := url.QueryEscape(key)
			encodedValue := url.QueryEscape(value)
			visibleValue := strings.ReplaceAll(encodedValue, "%2F", "/")
			if visibleValue == encodedValue {
				continue
			}
			query = strings.Replace(query, encodedKey+"="+encodedValue, encodedKey+"="+visibleValue, 1)
		}
	}
	return query
}

// gormConfig is the GORM configuration of the application's connection.
// TranslateError makes the dialector report driver errors such as a
// unique-key violation as GORM's portable errors (gorm.ErrDuplicatedKey),
// which callers test with errors.Is: without it those checks never match on
// MySQL or PostgreSQL.
func (m *Mysql) gormConfig() *gorm.Config {
	return &gorm.Config{
		Logger: &logger.GormLogger{SlowThreshold: m.GetSlowThreshold()},
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
		TranslateError: true,
	}
}

// GetSlowThreshold returns the duration from which a query is logged as
// slow.
func (m *Mysql) GetSlowThreshold() time.Duration {
	return time.Duration(m.Config.SlowThreshold) * time.Millisecond
}

// ConnectMysql is ConnectDatabase.
func ConnectMysql(m Mysql) (*gorm.DB, error) {
	return ConnectDatabase(m)
}

// ConnectDatabase opens the connection pool m describes, with the
// application's GORM configuration.
func ConnectDatabase(m Mysql) (*gorm.DB, error) {
	if m.Config.Dbname == "" {
		return nil, errors.New("database name is empty")
	}
	var dialector gorm.Dialector
	switch m.Driver() {
	case DriverMySQL:
		dialector = mysql.New(mysql.Config{DSN: m.Dsn()})
	case DriverPostgres:
		dialector = postgres.Open(m.Dsn())
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", m.Config.Driver)
	}
	db, err := gorm.Open(dialector, m.gormConfig())
	if err != nil {
		return nil, err
	}
	sqldb, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxIdleConns(m.Config.MaxIdleConns)
	sqldb.SetMaxOpenConns(m.Config.MaxOpenConns)
	if m.Config.ConnMaxLifetime > 0 {
		sqldb.SetConnMaxLifetime(time.Duration(m.Config.ConnMaxLifetime) * time.Second)
	}
	if m.Config.ConnMaxIdleTime > 0 {
		sqldb.SetConnMaxIdleTime(time.Duration(m.Config.ConnMaxIdleTime) * time.Second)
	}
	return db, nil
}
