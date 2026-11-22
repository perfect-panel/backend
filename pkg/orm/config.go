package orm

import (
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// ParseDSN parses a MySQL or URL-form DSN with the default parameters for
// DefaultLocation; it is ParseDSNIn(dsn, "").
func ParseDSN(dsn string) *Config {
	return ParseDSNIn(dsn, "")
}

// ParseDSNIn parses a MySQL or URL-form DSN. A DSN without parameters gets
// the default ones for location (DefaultLocation when empty). It returns nil
// for a DSN it cannot parse.
func ParseDSNIn(dsn, location string) *Config {
	if cfg := parseURLDSN(dsn, location); cfg != nil {
		return cfg
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil
	}
	query := DefaultMySQLQuery(location)
	if formattedQuery := MySQLDSNRawQuery(cfg.FormatDSN()); formattedQuery != "" {
		query = withDefaultMySQLParams(formattedQuery)
	}
	// FormatDSN omits false-valued booleans, so preserve an explicit caller
	// override before this parsed configuration is written into YAML.
	if originalQuery := MySQLDSNRawQuery(dsn); originalQuery != "" {
		if original, parseErr := url.ParseQuery(originalQuery); parseErr == nil {
			if values, configured := original["interpolateParams"]; configured {
				parsed, parseErr := url.ParseQuery(query)
				if parseErr == nil {
					parsed["interpolateParams"] = values
					query = parsed.Encode()
				}
			}
		}
	}
	return &Config{
		Driver:          DriverMySQL,
		Addr:            cfg.Addr,
		Dbname:          cfg.DBName,
		Username:        cfg.User,
		Password:        cfg.Passwd,
		Config:          query,
		MaxIdleConns:    10,
		MaxOpenConns:    10,
		ConnMaxLifetime: DefaultConnMaxLifetimeSeconds,
		ConnMaxIdleTime: DefaultConnMaxIdleTimeSeconds,
		SlowThreshold:   DefaultSlowThresholdMs,
	}
}

// MySQLDSNRawQuery extracts only the parameters following the database name.
// Looking for the first or last question mark in the full DSN is incorrect
// because MySQL passwords may contain one without escaping.
func MySQLDSNRawQuery(dsn string) string {
	databaseSeparator := strings.LastIndex(dsn, "/")
	if databaseSeparator < 0 {
		return ""
	}
	querySeparator := strings.IndexByte(dsn[databaseSeparator+1:], '?')
	if querySeparator < 0 {
		return ""
	}
	return dsn[databaseSeparator+querySeparator+2:]
}

func parseURLDSN(dsn, location string) *Config {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return nil
	}
	driver := NormalizeDriver(u.Scheme)
	if driver != DriverMySQL && driver != DriverPostgres {
		return nil
	}
	password, _ := u.User.Password()
	query := u.RawQuery
	if query == "" {
		if driver == DriverPostgres {
			query = DefaultPostgresQuery(location)
		} else {
			query = DefaultMySQLQuery(location)
		}
	} else if driver == DriverMySQL {
		query = withDefaultMySQLParams(query)
	}
	return &Config{
		Driver:          driver,
		Addr:            u.Host,
		Dbname:          strings.TrimPrefix(u.Path, "/"),
		Username:        u.User.Username(),
		Password:        password,
		Config:          query,
		MaxIdleConns:    10,
		MaxOpenConns:    10,
		ConnMaxLifetime: DefaultConnMaxLifetimeSeconds,
		ConnMaxIdleTime: DefaultConnMaxIdleTimeSeconds,
		SlowThreshold:   DefaultSlowThresholdMs,
	}
}
