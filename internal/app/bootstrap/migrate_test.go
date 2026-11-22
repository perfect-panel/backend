package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/app/migration/schema"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// stubMigration replaces the schema migration with one that returns err,
// until the test ends.
func stubMigration(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	previous := migrateSchema
	migrateSchema = func(string, string) error {
		calls++
		return err
	}
	t.Cleanup(func() { migrateSchema = previous })
	return &calls
}

func configWithAdministrator(email, password string) func() config.Config {
	var c config.Config
	c.Administrator.Email = email
	c.Administrator.Password = password
	return func() config.Config { return c }
}

// The first administrator is seeded on every start, also when the schema is
// already current: a seed that failed on the first start used to be retried
// never, because only a migration that changed the schema seeded.
func TestMigrateSeedsTheFirstAdministratorOnEveryStart(t *testing.T) {
	logtest.Discard(t)
	for name, migration := range map[string]error{"schema changed": nil, "schema current": schema.NoChange} {
		t.Run(name, func(t *testing.T) {
			calls := stubMigration(t, migration)
			admins := &administrators{}

			err := Migrate(context.Background(), &Dependencies{Config: configWithAdministrator("admin@example.com", "configured-secret"), Administrators: admins})

			if err != nil {
				t.Fatalf("Migrate() = %v", err)
			}
			if *calls != 1 {
				t.Fatalf("migration ran %d times, want once", *calls)
			}
			if len(admins.created) != 1 || admins.created[0].email != "admin@example.com" || admins.created[0].password != "configured-secret" {
				t.Fatalf("seeded %+v, want the configured administrator", admins.created)
			}
		})
	}
}

// A migration that fails is reported and seeds nothing.
func TestMigrateDoesNotSeedAfterAFailedMigration(t *testing.T) {
	logtest.Discard(t)
	cause := errors.New("dirty database version 42")
	stubMigration(t, cause)
	admins := &administrators{}

	err := Migrate(context.Background(), &Dependencies{Config: configWithAdministrator("admin@example.com", ""), Administrators: admins})

	if !errors.Is(err, cause) {
		t.Fatalf("Migrate() = %v, want the migration failure", err)
	}
	if len(admins.created) != 0 {
		t.Fatalf("seeded %+v after a failed migration", admins.created)
	}
}

// Once the administrator exists, a password still in the configuration file
// seeds nothing and is flagged; a file without one is left alone.
func TestSeedFirstAdministratorFlagsAStalePasswordInTheFile(t *testing.T) {
	for password, flagged := range map[string]bool{"configured-secret": true, "": false} {
		logs := logtest.NewCollector(t)
		admins := &administrators{hasAccounts: true}

		if err := seedFirstAdministrator(context.Background(), &Dependencies{Administrators: admins}, "admin@example.com", password); err != nil {
			t.Fatalf("seedFirstAdministrator() = %v", err)
		}

		if got := strings.Contains(logs.String(), "Administrator.Password is still set"); got != flagged {
			t.Fatalf("password %q: flagged = %t, want %t: %s", password, got, flagged, logs.String())
		}
		if len(admins.created) != 0 {
			t.Fatalf("seeded %+v although accounts exist", admins.created)
		}
	}
}

// ReloadAll reloads every subsystem in startup order and stops at the first
// failure; it runs no migration.
func TestReloadAllReloadsEverySubsystemInStartupOrder(t *testing.T) {
	logtest.Discard(t)
	calls := stubMigration(t, nil)
	var order []Subsystem
	previous := loaders
	loaders = map[Subsystem]func(context.Context, *Dependencies) error{}
	for subsystem := range previous {
		loaders[subsystem] = func(_ context.Context, _ *Dependencies) error {
			order = append(order, subsystem)
			if subsystem == SubsystemMobile {
				return errors.New("mobile settings unreadable")
			}
			return nil
		}
	}
	t.Cleanup(func() { loaders = previous })

	err := ReloadAll(context.Background(), &Dependencies{})

	if err == nil || !strings.Contains(err.Error(), "mobile") {
		t.Fatalf("ReloadAll() = %v, want the mobile failure", err)
	}
	want := startupOrder[:9] // up to and including mobile
	if len(order) != len(want) {
		t.Fatalf("reloaded %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("reloaded %v, want the startup order %v", order, want)
		}
	}
	if *calls != 0 {
		t.Fatal("ReloadAll ran the migration")
	}
}
