package cmd

import (
	"github.com/perfect-panel/backend/internal/app/migration/mysql2postgres"
	"github.com/spf13/cobra"
)

var mysql2postgresConfig = mysql2postgres.DefaultConfig()

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.AddCommand(mysql2postgresCmd)

	flags := mysql2postgresCmd.Flags()
	flags.StringVar(&mysql2postgresConfig.MySQLDSN, "mysql", "", "source MySQL/MariaDB DSN")
	flags.StringVar(&mysql2postgresConfig.PostgresDSN, "postgres", "", "target PostgreSQL DSN")
	flags.StringVar(&mysql2postgresConfig.Schema, "schema", mysql2postgresConfig.Schema, "target PostgreSQL schema")
	flags.StringVar(&mysql2postgresConfig.Tables, "tables", "", "comma-separated table allowlist")
	flags.StringVar(&mysql2postgresConfig.Exclude, "exclude", "", "comma-separated table denylist")
	flags.BoolVar(&mysql2postgresConfig.Truncate, "truncate", false, "truncate common target tables before copy")
	flags.BoolVar(&mysql2postgresConfig.Yes, "yes", false, "confirm destructive operations")
	flags.BoolVar(&mysql2postgresConfig.DryRun, "dry-run", false, "print plan without copying data")
	flags.IntVar(&mysql2postgresConfig.BatchSize, "batch-size", mysql2postgresConfig.BatchSize, "rows per progress log")
	flags.StringVar(&mysql2postgresConfig.Location, "location", mysql2postgresConfig.Location, "IANA zone the MySQL DATETIME values are in: the panel's AppLocation")
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "database migration tools",
}

var mysql2postgresCmd = &cobra.Command{
	Use:          "mysql2postgres",
	Short:        "migrate data from MySQL/MariaDB to PostgreSQL",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mysql2postgres.Migrate(cmd.Context(), mysql2postgresConfig)
	},
}
