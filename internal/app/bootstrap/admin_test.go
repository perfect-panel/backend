package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// A configured password is used as given; the documented installs configure
// none, and those must not fall back to the published default.
func TestInitialAdminPasswordGeneratesOnlyWhenUnset(t *testing.T) {
	for _, configured := range []string{"configured-secret", defaultAdminPassword} {
		if got, generated := initialAdminPassword(configured); got != configured || generated {
			t.Fatalf("configured password %q replaced with %q (generated %t)", configured, got, generated)
		}
	}
	if got, generated := initialAdminPassword(""); !generated || got == "" || got == defaultAdminPassword || len(got) < 16 {
		t.Fatalf("initialAdminPassword(\"\") = %q, %t, want a generated password", got, generated)
	}
	first, _ := initialAdminPassword("")
	second, _ := initialAdminPassword("")
	if first == second {
		t.Fatal("generated passwords repeat")
	}
}

// administrators stands in for identity's administrator accounts: it creates
// the first administrator unless the database already holds an account, and
// answers the password check from withPassword.
type administrators struct {
	hasAccounts  bool
	createErr    error
	created      []seededAdministrator
	withPassword map[string][]*user.User
	findErr      error
}

type seededAdministrator struct{ email, password string }

var _ Administrators = (*administrators)(nil)

func (a *administrators) CreateInitialAdministrator(_ context.Context, email, password string) (bool, error) {
	if a.createErr != nil {
		return false, a.createErr
	}
	if a.hasAccounts {
		return false, nil
	}
	a.created = append(a.created, seededAdministrator{email: email, password: password})
	return true, nil
}

func (a *administrators) FindAdministratorsWithPassword(_ context.Context, password string) ([]*user.User, error) {
	if a.findErr != nil {
		return nil, a.findErr
	}
	return a.withPassword[password], nil
}

// printed captures the standard logger, which prints a generated password
// outside the structured logger's redaction.
func printed(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buf
}

// The configured administrator is created on a database without accounts,
// with the configured password or a generated one. A generated password is
// printed once, and only when the account it signs in to exists.
func TestSeedFirstAdministratorPrintsOnlyTheGeneratedPasswordOfACreatedAccount(t *testing.T) {
	for name, tc := range map[string]struct {
		configured  string
		hasAccounts bool
		wantCreated bool
		wantPrinted bool
	}{
		"configured password": {configured: "configured-secret", wantCreated: true},
		"generated password":  {wantCreated: true, wantPrinted: true},
		"existing accounts":   {hasAccounts: true},
	} {
		t.Run(name, func(t *testing.T) {
			logs := logtest.NewCollector(t)
			out := printed(t)
			admins := &administrators{hasAccounts: tc.hasAccounts}

			if err := seedFirstAdministrator(context.Background(), &Dependencies{Administrators: admins}, "admin@example.com", tc.configured); err != nil {
				t.Fatalf("seedFirstAdministrator() = %v", err)
			}

			if created := len(admins.created) == 1; created != tc.wantCreated {
				t.Fatalf("created = %+v, want an administrator: %t", admins.created, tc.wantCreated)
			}
			if tc.wantCreated {
				seeded := admins.created[0]
				if seeded.email != "admin@example.com" || seeded.password == "" || (tc.configured != "" && seeded.password != tc.configured) {
					t.Fatalf("seeded %+v, want admin@example.com with the configured or a generated password", seeded)
				}
				if tc.wantPrinted != strings.Contains(out.String(), seeded.password) {
					t.Fatalf("printed %q, want the password printed: %t", out.String(), tc.wantPrinted)
				}
			}
			if !tc.wantPrinted && out.Len() != 0 {
				t.Fatalf("printed %q, want nothing", out.String())
			}
			if success := strings.Contains(logs.String(), "Create admin user success"); success != tc.wantCreated {
				t.Fatalf("log = %s, want the creation reported: %t", logs.String(), tc.wantCreated)
			}
		})
	}
}

// A failed creation fails the migration under DatabaseInsertError, keeps the
// cause reachable and prints no password.
func TestSeedFirstAdministratorReportsAFailedCreation(t *testing.T) {
	logs := logtest.NewCollector(t)
	out := printed(t)
	cause := errors.New("database down")

	err := seedFirstAdministrator(context.Background(), &Dependencies{Administrators: &administrators{createErr: cause}}, "admin@example.com", "")

	if !errors.Is(err, cause) || xerr.CodeOf(err) != xerr.DatabaseInsertError {
		t.Fatalf("seedFirstAdministrator() = %v (code %d), want the cause under DatabaseInsertError", err, xerr.CodeOf(err))
	}
	if !strings.Contains(logs.String(), "CreateAdminUser error") || !strings.Contains(logs.String(), "database down") {
		t.Fatalf("log = %s, want the failure logged with its cause", logs.String())
	}
	if out.Len() != 0 {
		t.Fatalf("printed %q after a failed creation", out.String())
	}
}

// Every administrator identity finds on the published default password is
// flagged; a failed lookup is only logged.
func TestWarnDefaultAdminPasswordFlagsTheAdministratorsOnTheDefault(t *testing.T) {
	logs := logtest.NewCollector(t)
	admins := &administrators{withPassword: map[string][]*user.User{defaultAdminPassword: {
		{Id: 1, AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "admin@ppanel.dev"}}},
	}}}

	WarnDefaultAdminPassword(context.Background(), &Dependencies{Administrators: admins})

	content := logs.String()
	if !strings.Contains(content, "default password") || !strings.Contains(content, `"user_id":1`) {
		t.Fatalf("default-password administrator not flagged:\n%s", content)
	}

	logs.Reset()
	WarnDefaultAdminPassword(context.Background(), &Dependencies{Administrators: &administrators{findErr: errors.New("database down")}})
	if content := logs.String(); !strings.Contains(content, "Query admin users error") || strings.Contains(content, "default password") {
		t.Fatalf("log = %s, want only the failed lookup", content)
	}
}
