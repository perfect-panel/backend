// Package schema migrates the database schema with the SQL migrations of both
// dialects embedded in the binary, and seeds the first administrator of a
// fresh installation.
//
// # Time columns in the PostgreSQL schema
//
// The PostgreSQL migrations mix two column types. The core tables, ported
// from MySQL's DATETIME(3), use TIMESTAMP(3) (without time zone): a wall
// clock, stored as the application writes it, in the session time zone the
// connection parameters pin (pkg/orm adds TimeZone=<AppLocation> to every
// DSN that names none). Seventeen columns added later — servers'
// last_reported_at, the subscription_*, domain_event_*, user_wallet,
// telegram_topic and task_error tables — use TIMESTAMPTZ: an instant, which
// PostgreSQL converts to the session zone on read.
//
// Both agree as long as the session zone is the application's zone, which
// the DSN now guarantees; a DSN without a zone used to read TIMESTAMP
// columns back labelled UTC while TIMESTAMPTZ columns stayed right, so the
// same table carried two meanings. The existing columns are not migrated:
// rewriting TIMESTAMP(3) columns of populated tables to TIMESTAMPTZ takes a
// full-table rewrite per column and a matching MySQL change for nothing
// the pinned zone does not already give.
//
// For a new column, use TIMESTAMPTZ (and DATETIME(3) on MySQL): it stores
// an instant whatever the session zone is, needs no zone convention, and
// reads back in the session zone like the older columns. Use TIMESTAMP only
// for a calendar wall clock that must not shift with the zone.
package schema

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
)

//go:embed database/mysql/*.sql database/postgres/*.sql
var sqlFiles embed.FS

// NoChange is the error Up returns when the schema is already current.
var NoChange = migrate.ErrNoChange

// pgxScheme selects golang-migrate's pgx/v5 database driver. PostgreSQL
// migrations run on pgx, the driver GORM already uses for the application's
// own connection, so golang-migrate's lib/pq driver is not linked in.
const pgxScheme = "pgx5"

// Up applies every pending migration and always releases the source and
// database drivers opened by golang-migrate. Callers previously invoked
// Migrate(...).Up() directly and leaked the migration driver's independent
// database connection after startup or installation.
func Up(driver, dsn string) error {
	client, err := Migrate(driver, dsn)
	if err != nil {
		return err
	}
	return upAndClose(client)
}

type migrationRunner interface {
	Up() error
	Close() (sourceErr, databaseErr error)
}

func upAndClose(client migrationRunner) error {
	migrateErr := client.Up()
	sourceErr, databaseErr := client.Close()
	closeErr := errors.Join(sourceErr, databaseErr)
	if closeErr == nil {
		return migrateErr
	}

	closeErr = fmt.Errorf("close migration drivers: %w", closeErr)
	if migrateErr == nil || errors.Is(migrateErr, migrate.ErrNoChange) {
		return closeErr
	}
	return errors.Join(migrateErr, closeErr)
}

// Migrate opens a migration client for the dialect's embedded migrations. It
// connects to the database, so an unreachable database is reported here.
func Migrate(driver, dsn string) (*migrate.Migrate, error) {
	driver = orm.NormalizeDriver(driver)
	sourcePath := "database/mysql"
	var databaseURL string
	switch driver {
	case orm.DriverMySQL:
		databaseURL = ensureScheme(orm.DriverMySQL, dsn)
	case orm.DriverPostgres:
		sourcePath = "database/postgres"
		databaseURL = postgresMigrationURL(dsn)
	default:
		logger.Errorf("[Migrate] unsupported database driver: %s", driver)
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}
	d, err := iofs.New(sqlFiles, sourcePath)
	if err != nil {
		logger.Errorf("[Migrate] iofs.New error: %v", err.Error())
		return nil, fmt.Errorf("open embedded %s migrations: %w", driver, err)
	}
	client, err := migrate.NewWithSourceInstance("iofs", d, databaseURL)
	if err != nil {
		logger.Errorf("[Migrate] NewWithSourceInstance error: %v", err.Error())
		return nil, errors.Join(fmt.Errorf("open %s migration database: %w", driver, err), d.Close())
	}
	return client, nil
}

func ensureScheme(driver, dsn string) string {
	if strings.Contains(dsn, "://") {
		return dsn
	}
	return fmt.Sprintf("%s://%s", driver, dsn)
}

// postgresMigrationURL addresses a PostgreSQL DSN to the pgx/v5 migration
// driver, which registers the pgx5 scheme and connects with the equivalent
// postgres:// URL. Everything after the scheme, including the x-migrations-*
// options, is kept as given, so the migrations table stays schema_migrations
// unless the DSN says otherwise.
func postgresMigrationURL(dsn string) string {
	for _, scheme := range []string{orm.DriverPostgres + "://", orm.DriverPostgres2 + "://", pgxScheme + "://"} {
		if len(dsn) >= len(scheme) && strings.EqualFold(dsn[:len(scheme)], scheme) {
			return pgxScheme + "://" + dsn[len(scheme):]
		}
	}
	return ensureScheme(pgxScheme, dsn)
}
