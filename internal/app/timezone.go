package app

import (
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
)

// warnDatabaseTimeZone reports a database session zone that differs from
// AppLocation, or that cannot be determined. Stored times and the per-day
// statistics use the session zone, so the two must match; they are not
// aligned automatically, because changing the session zone of a database
// with data reinterprets every stored time.
func warnDatabaseTimeZone(c config.Config) {
	session := orm.Mysql{Config: c.DatabaseConfig()}.SessionLocation()
	if session == "" {
		// Only connection parameters that do not parse leave the zone
		// unknown: the DSN builders name one otherwise. GORM then reads
		// PostgreSQL timestamps back labelled UTC, hours off the zone they
		// were written in, so this is not a state to run in unnoticed.
		logger.Errorw("[Database] the database session time zone is unknown: the connection parameters do not parse; "+
			"write them as key=value pairs and set the zone (MySQL loc, PostgreSQL TimeZone) to AppLocation",
			logger.Field("database_parameters", c.DatabaseConfig().Config), logger.Field("app_location", c.AppLocation))
		return
	}
	if session == c.AppLocation {
		return
	}
	logger.Errorw("[Database] the database time zone differs from AppLocation: stored times and daily statistics use the database zone; "+
		"set the zone in the database parameters (MySQL loc, PostgreSQL TimeZone) to AppLocation only on an empty database or after converting the stored times",
		logger.Field("database_time_zone", session), logger.Field("app_location", c.AppLocation))
}
