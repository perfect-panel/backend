package startup

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// checked builds the startup service of a deployment with siteHost and the
// enabled sign-in methods.
func checked(t *testing.T, siteHost string, methods ...string) *Service {
	t.Helper()
	env := identitytest.New(t)
	for _, method := range methods {
		env.EnableMethod(t, method, "{}")
	}
	return NewService(Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Auths: env.Store.Auth(), Store: env.Store,
		SiteHost: func() string { return siteHost },
	})
}

// Apple and Telegram send the browser to a client-supplied redirect that only
// a configured site host pins reliably; the start-up check names them when
// they are enabled without one, and stays silent when the site host is set
// or only methods that keep their own callback (GitHub) are enabled.
func TestWarnUnpinnedOAuthRedirectsNamesTheMethodsNeedingASiteHost(t *testing.T) {
	for name, tc := range map[string]struct {
		siteHost string
		methods  []string
		want     string
	}{
		"apple and telegram without a site host": {"", []string{"apple", "telegram", "github"}, `"methods":["apple","telegram"]`},
		"telegram alone":                         {"", []string{"telegram"}, `"methods":["telegram"]`},
		"site host configured":                   {"https://panel.example", []string{"apple", "telegram"}, ""},
		"only self-pinning methods":              {"", []string{"github", "google"}, ""},
		"nothing enabled":                        {"", nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			svc := checked(t, tc.siteHost, tc.methods...)
			logs := logtest.NewCollector(t)
			if err := svc.WarnUnpinnedOAuthRedirects(context.Background()); err != nil {
				t.Fatalf("WarnUnpinnedOAuthRedirects() error = %v", err)
			}
			entries := logs.String()
			if tc.want == "" && strings.Contains(entries, "no site host") {
				t.Fatalf("logs = %q, want no warning", entries)
			}
			if tc.want != "" && (!strings.Contains(entries, "no site host is configured") || !strings.Contains(entries, tc.want)) {
				t.Fatalf("logs = %q, want the warning naming %s", entries, tc.want)
			}
		})
	}
}

// A disabled method needs no site host.
func TestWarnUnpinnedOAuthRedirectsIgnoresDisabledMethods(t *testing.T) {
	env := identitytest.New(t)
	if err := env.DB.Exec("INSERT INTO auth_method (method, config, enabled) VALUES (?, ?, ?)", "apple", "{}", false).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(Deps{Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Auths: env.Store.Auth(), Store: env.Store, SiteHost: func() string { return "" }})
	logs := logtest.NewCollector(t)
	if err := svc.WarnUnpinnedOAuthRedirects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if entries := logs.String(); strings.Contains(entries, "no site host") {
		t.Fatalf("logs = %q, want no warning for a disabled method", entries)
	}
}

// Administrators whose password hash is still a legacy format are named by
// id, never by address; administrators on argon2id and members on legacy
// hashes are not.
func TestReportLegacyAdministratorPasswordsNamesOnlyIds(t *testing.T) {
	f := newFixture(t)
	admin, member := true, false
	legacy := f.account(t, &user.User{IsAdmin: &admin, Password: "5f4dcc3b5aa765d61d8327deb882cf99", Algo: "md5"}, "email", "legacy-admin@example.com")
	pbkdf := f.account(t, &user.User{IsAdmin: &admin, Password: "$pbkdf2-sha512$salt$hash", Algo: "default"}, "email", "pbkdf-admin@example.com")
	f.account(t, &user.User{IsAdmin: &admin, Password: password.EncodePassWord("strong"), Algo: password.PasswordAlgoArgon2id}, "email", "modern-admin@example.com")
	f.account(t, &user.User{IsAdmin: &member, Password: "5f4dcc3b5aa765d61d8327deb882cf99", Algo: "md5"}, "email", "legacy-member@example.com")
	logs := logtest.NewCollector(t)

	if err := f.svc.ReportLegacyAdministratorPasswords(context.Background()); err != nil {
		t.Fatalf("ReportLegacyAdministratorPasswords() error = %v", err)
	}
	entries := logs.String()
	if !strings.Contains(entries, "legacy password hash") {
		t.Fatalf("logs = %q, want the report", entries)
	}
	if strings.Contains(entries, "@example.com") {
		t.Fatalf("logs = %q, want no address", entries)
	}
	want := `"user_ids":[` + strconv.FormatInt(legacy.Id, 10) + `,` + strconv.FormatInt(pbkdf.Id, 10) + `]`
	if !strings.Contains(entries, want) {
		t.Fatalf("logs = %q, want %s", entries, want)
	}
}

// Nothing is reported when every administrator is on argon2id.
func TestReportLegacyAdministratorPasswordsIsSilentForModernHashes(t *testing.T) {
	f := newFixture(t)
	admin := true
	f.account(t, &user.User{IsAdmin: &admin, Password: password.EncodePassWord("strong"), Algo: password.PasswordAlgoArgon2id}, "email", "modern-admin@example.com")
	logs := logtest.NewCollector(t)
	if err := f.svc.ReportLegacyAdministratorPasswords(context.Background()); err != nil {
		t.Fatal(err)
	}
	if entries := logs.String(); strings.Contains(entries, "legacy password hash") {
		t.Fatalf("logs = %q, want no report", entries)
	}
}
