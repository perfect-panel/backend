package app

import (
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
)

func TestWarnDatabaseTimeZone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		driver   string
		location string
		params   string
		want     string // the logged complaint, "" for none
	}{
		{"defaults match the default location", orm.DriverMySQL, "Asia/Shanghai", "", ""},
		{"defaults differ from another location", orm.DriverMySQL, "Europe/Paris", "", "differs"},
		{"explicit parameters match", orm.DriverMySQL, "Europe/Paris", "charset=utf8mb4&parseTime=true&loc=Europe%2FParis", ""},
		// PostgreSQL parameters without a zone get the default zone in the
		// DSN, so they match the default location and differ from another.
		{"postgres parameters without a zone in the default location", orm.DriverPostgres, "Asia/Shanghai", "sslmode=require", ""},
		{"postgres parameters without a zone in another location", orm.DriverPostgres, "Europe/Paris", "sslmode=require", "differs"},
		{"postgres explicit zone matches", orm.DriverPostgres, "Europe/Paris", "sslmode=require&TimeZone=Europe/Paris", ""},
		// Parameters that do not parse leave the zone unknown, which the
		// startup used to pass over in silence.
		{"postgres parameters that do not parse", orm.DriverPostgres, "Asia/Shanghai", "sslmode=%zz", "unknown"},
		{"mysql parameters that do not parse", orm.DriverMySQL, "Asia/Shanghai", "loc=%zz", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := logtest.NewCollector(t)
			var c config.Config
			c.AppLocation = tc.location
			c.SetDatabaseConfig(orm.Config{Driver: tc.driver, Config: tc.params})
			warnDatabaseTimeZone(c)
			out := logs.String()
			if tc.want == "" {
				if out != "" {
					t.Fatalf("logged %s, want nothing", out)
				}
				return
			}
			if !strings.Contains(out, "database time zone") && !strings.Contains(out, "session time zone") || !strings.Contains(out, tc.want) {
				t.Fatalf("logged %q, want a time-zone complaint mentioning %q", out, tc.want)
			}
		})
	}
}
