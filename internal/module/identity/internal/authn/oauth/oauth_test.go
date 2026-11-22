package oauth

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthflow"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const testSecret = "oauth-test-secret"

// fakeProvider vouches for whatever identity the test sets.
type fakeProvider struct {
	identity oauthprovider.Identity
}

func (p *fakeProvider) AuthURL(redirect, state string) (string, error) {
	return "https://provider.example/authorize?" + url.Values{"redirect_uri": {redirect}, "state": {state}}.Encode(), nil
}

func (p *fakeProvider) Identify(context.Context, oauthprovider.Callback) (*oauthprovider.Identity, error) {
	identity := p.identity
	return &identity, nil
}

type fixture struct {
	*identitytest.Env
	svc      *Service
	provider *fakeProvider
	cfg      *Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	env.EnableMethod(t, "github", "{}")
	provider := &fakeProvider{identity: oauthprovider.Identity{Subject: "583231", Avatar: "https://cdn.example/a.png"}}
	cfg := &Config{Sessions: account.SessionConfig{Secret: testSecret, Lifetime: 3600}}
	flow := oauthflow.New(oauthflow.Deps{
		Auths: env.Store.Auth(),
		Redis: env.Redis,
		Providers: oauthprovider.Registry{
			"github": {State: true, New: func(string) (oauthprovider.Provider, error) { return provider, nil }},
		},
	})
	policy := registerpolicy.New(registerpolicy.Deps{Auths: env.Store.Auth(), Redis: env.Redis, Config: func() registerpolicy.Snapshot { return registerpolicy.Snapshot{} }})
	svc := NewService(Deps{Store: env.Store, Redis: env.Redis, Policy: policy, Flow: flow, Config: func() Config { return *cfg }})
	return &fixture{Env: env, svc: svc, provider: provider, cfg: cfg}
}

// signIn runs a whole sign-in: the authorization URL, then its callback.
func (f *fixture) signIn(t *testing.T, invite string) (*dto.LoginResponse, error) {
	t.Helper()
	ctx := identitytest.Context()
	started, err := f.svc.OAuthLogin(ctx, &dto.OAuthLoginRequest{Method: "github", Redirect: "https://panel.example/oauth"})
	if err != nil {
		t.Fatalf("OAuthLogin() error = %v", err)
	}
	parsed, err := url.Parse(started.Redirect)
	if err != nil {
		t.Fatal(err)
	}
	return f.svc.OAuthLoginGetToken(ctx, &dto.OAuthLoginGetTokenRequest{
		Method:   "github",
		Callback: map[string]any{"code": "authorization-code", "state": parsed.Query().Get("state")},
		Invite:   invite,
	})
}

func (f *fixture) loginAudits(t *testing.T, userID int64) []log.Login {
	t.Helper()
	var audits []log.Login
	for _, row := range f.Logs(t, log.TypeLogin, userID) {
		var content log.Login
		if err := json.Unmarshal([]byte(row.Content), &content); err != nil {
			t.Fatal(err)
		}
		audits = append(audits, content)
	}
	return audits
}

func (f *fixture) account(t *testing.T, authType, identifier string, verified bool) *user.User {
	t.Helper()
	enabled := true
	u := &user.User{Enable: &enabled, ReferCode: "REF-" + identifier}
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Create(&user.AuthMethods{UserId: u.Id, AuthType: authType, AuthIdentifier: identifier, Verified: verified}).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// An identity no account holds registers one, with the verified email the
// provider reported bound too.
func TestOAuthRegistrationCreatesTheAccount(t *testing.T) {
	f := newFixture(t)
	f.provider.identity.Email = "Octo@Example.com"

	resp, err := f.signIn(t, "")
	if err != nil {
		t.Fatalf("sign-in error = %v", err)
	}
	users := f.Users(t)
	if len(users) != 1 || users[0].Avatar != "https://cdn.example/a.png" || users[0].ReferCode == "" {
		t.Fatalf("accounts = %+v", users)
	}
	created := users[0]
	claims, err := usersession.Validate(context.Background(), f.Redis, testSecret, resp.Token)
	if err != nil || claims.UserID != created.Id {
		t.Fatalf("session = %+v, %v", claims, err)
	}
	identities := f.Identities(t, created.Id)
	if len(identities) != 2 ||
		identities[0].AuthType != "github" || identities[0].AuthIdentifier != "583231" || !identities[0].Verified ||
		identities[1].AuthType != "email" || identities[1].AuthIdentifier != "octo@example.com" || !identities[1].Verified {
		t.Fatalf("identities = %+v", identities)
	}
	if events := f.Events(t, account.RegisteredTopic); len(events) != 1 {
		t.Fatalf("registration events = %d, want 1", len(events))
	}
	if rows := f.Logs(t, log.TypeRegister, created.Id); len(rows) != 1 {
		t.Fatalf("registration audits = %d, want 1", len(rows))
	}
	if audits := f.loginAudits(t, created.Id); len(audits) != 1 || !audits[0].Success || audits[0].Method != "github" {
		t.Fatalf("login audits = %+v", audits)
	}
}

func TestOAuthSignsInTheAccountHoldingTheIdentity(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "github", "583231", true)

	resp, err := f.signIn(t, "")
	if err != nil {
		t.Fatalf("sign-in error = %v", err)
	}
	claims, err := usersession.Validate(context.Background(), f.Redis, testSecret, resp.Token)
	if err != nil || claims.UserID != owner.Id {
		t.Fatalf("session = %+v, %v; want the owner's", claims, err)
	}
	if n := len(f.Users(t)); n != 1 {
		t.Fatalf("accounts = %d, want no new one", n)
	}
	if audits := f.loginAudits(t, owner.Id); len(audits) != 1 || !audits[0].Success {
		t.Fatalf("login audits = %+v", audits)
	}
}

// A guest checkout could name someone's provider id before they ever signed
// in. Such a binding is unverified, and proving the identity later must not
// open the account that claimed it.
func TestOAuthRefusesAnUnverifiedBinding(t *testing.T) {
	f := newFixture(t)
	planted := f.account(t, "github", "583231", false)

	_, err := f.signIn(t, "")
	assertCode(t, err, xerr.UserExist)
	if audits := f.loginAudits(t, planted.Id); len(audits) != 0 {
		t.Fatalf("login audits = %+v, want none for the planted account", audits)
	}
}

// A registration rolled back leaves no account and no login audit for the
// user id it had been given.
func TestOAuthFailedRegistrationAuditsNothing(t *testing.T) {
	f := newFixture(t)
	if err := f.DB.Exec(`CREATE TRIGGER reject_registration_event BEFORE INSERT ON domain_event_outbox BEGIN SELECT RAISE(FAIL, 'test failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.signIn(t, ""); err == nil {
		t.Fatal("sign-in succeeded without its registration event")
	}
	if users := f.Users(t); len(users) != 0 {
		t.Fatalf("accounts = %+v, want none", users)
	}
	var audits int64
	f.DB.Model(&log.SystemLog{}).Count(&audits)
	if audits != 0 {
		t.Fatalf("audit rows = %d, want none", audits)
	}
}

func TestOAuthRegistrationFollowsTheInvitePolicy(t *testing.T) {
	f := newFixture(t)
	referer := f.account(t, "email", "referer@example.com", true)
	f.cfg.InviteForced = true

	_, err := f.signIn(t, "")
	assertCode(t, err, xerr.InviteCodeError)
	_, err = f.signIn(t, "NO-SUCH-CODE")
	assertCode(t, err, xerr.InviteCodeError)
	if _, err := f.signIn(t, referer.ReferCode); err != nil {
		t.Fatalf("sign-in with a valid invite: %v", err)
	}
	var created user.User
	if err := f.DB.Where("id <> ?", referer.Id).First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.RefererId != referer.Id {
		t.Fatalf("referer = %d, want %d", created.RefererId, referer.Id)
	}
}

// The provider's email must not take over another account's address.
func TestOAuthRegistrationRefusesAnotherAccountsEmail(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "octo@example.com", true)
	f.provider.identity.Email = "octo@example.com"

	_, err := f.signIn(t, "")
	assertCode(t, err, xerr.UserExist)
	if n := len(f.Users(t)); n != 1 {
		t.Fatalf("accounts = %d, want only the existing one", n)
	}
}

func TestOAuthCallbackMustBeAnObject(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.OAuthLoginGetToken(identitytest.Context(), &dto.OAuthLoginGetTokenRequest{Method: "github", Callback: "not-an-object"})
	assertCode(t, err, xerr.InvalidParams)
}

// A method with no OAuth provider, such as email, has no authorization URL.
func TestOAuthLoginRefusesMethodsWithoutAProvider(t *testing.T) {
	f := newFixture(t)
	f.EnableMethod(t, "linkedin", "{}")
	_, err := f.svc.OAuthLogin(identitytest.Context(), &dto.OAuthLoginRequest{Method: "linkedin", Redirect: "https://panel.example/oauth"})
	assertCode(t, err, xerr.AuthenticatorNotSupportedError)
	_, err = f.svc.OAuthLogin(identitytest.Context(), &dto.OAuthLoginRequest{Method: "facebook", Redirect: "https://panel.example/oauth"})
	assertCode(t, err, xerr.GetAuthenticatorError)
}
