package authn

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const testSecret = "authn-test-secret"

// openSnapshot enables every sign-in method and registration without
// checks, like a fresh installation.
func openSnapshot() Snapshot {
	return Snapshot{
		JWTAccessSecret: testSecret, JWTAccessExpire: 3600,
		EmailEnabled: true, MobileEnabled: true, DeviceEnabled: true,
	}
}

type fixture struct {
	*identitytest.Env
	svc *Service
	cfg *Snapshot
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	cfg := openSnapshot()
	f := &fixture{Env: env, cfg: &cfg}
	f.svc = NewService(Deps{Store: env.Store, Redis: env.Redis, Config: func() Snapshot { return *f.cfg }})
	return f
}

// account creates an enabled account with a password and one identity.
func (f *fixture) account(t *testing.T, authType, identifier, plain string) *user.User {
	t.Helper()
	enabled := true
	u := &user.User{Password: password.EncodePassWord(plain), Algo: password.PasswordAlgoArgon2id, Enable: &enabled, ReferCode: "REF" + identifier}
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Create(&user.AuthMethods{UserId: u.Id, AuthType: authType, AuthIdentifier: identifier, Verified: true}).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *fixture) saveCode(t *testing.T, key, code string) {
	t.Helper()
	if err := verification.SaveVerificationCode(context.Background(), f.Redis, key, code, time.Minute); err != nil {
		t.Fatal(err)
	}
}

// sessionUser returns the user a token signs in as, failing unless the
// session is live.
func (f *fixture) sessionUser(t *testing.T, token string) int64 {
	t.Helper()
	claims, err := usersession.Validate(context.Background(), f.Redis, testSecret, token)
	if err != nil {
		t.Fatalf("session is not live: %v", err)
	}
	return claims.UserID
}

// loginAudits returns the login audit outcomes recorded for the account.
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

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// registered checks what a registration wrote: one account with its refer
// code, the identities, the registration event and audit, and the audit of
// the sign-in that followed.
func (f *fixture) registered(t *testing.T, token, method string) (user.User, []user.AuthMethods) {
	t.Helper()
	users := f.Users(t)
	if len(users) != 1 {
		t.Fatalf("accounts = %d, want 1", len(users))
	}
	created := users[0]
	if len(created.ReferCode) < 2 || created.ReferCode[0] != 'u' {
		t.Fatalf("refer code = %q, want a generated invite code", created.ReferCode)
	}
	if got := f.sessionUser(t, token); got != created.Id {
		t.Fatalf("session signs in as %d, want %d", got, created.Id)
	}
	events := f.Events(t, account.RegisteredTopic)
	if len(events) != 1 || events[0].EventKey != itoa(created.Id) {
		t.Fatalf("registration events = %+v", events)
	}
	registrations := f.Logs(t, log.TypeRegister, created.Id)
	if len(registrations) != 1 {
		t.Fatalf("registration audits = %d, want 1", len(registrations))
	}
	var audit log.Register
	if err := json.Unmarshal([]byte(registrations[0].Content), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.AuthMethod != method || audit.Identifier != logger.RedactedValue || audit.RegisterIP != identitytest.ClientIP || audit.UserAgent != identitytest.UserAgent {
		t.Fatalf("registration audit = %+v", audit)
	}
	logins := f.loginAudits(t, created.Id)
	if len(logins) != 1 || !logins[0].Success || logins[0].Method != method || logins[0].LoginIP != identitytest.ClientIP {
		t.Fatalf("login audits = %+v", logins)
	}
	return created, f.Identities(t, created.Id)
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestEmailRegistrationWritesTheAccountAndSignsIn(t *testing.T) {
	f := newFixture(t)
	f.cfg.EmailVerifyEnabled = true
	f.saveCode(t, verification.EmailCodeKey(auth.Register, "new@example.com"), "123456")

	resp, err := f.svc.UserRegister(identitytest.Context(), &dto.UserRegisterRequest{Email: " New@Example.com ", Password: "password-1", Code: "123456"})
	if err != nil {
		t.Fatalf("UserRegister() error = %v", err)
	}
	created, identities := f.registered(t, resp.Token, "email")
	if !password.MultiPasswordVerify(created.Algo, created.Salt, "password-1", created.Password) {
		t.Fatal("the password was not stored")
	}
	if len(identities) != 1 || identities[0].AuthType != "email" || identities[0].AuthIdentifier != "new@example.com" || !identities[0].Verified {
		t.Fatalf("identities = %+v", identities)
	}
	// The code is spent.
	if _, err := f.svc.UserRegister(identitytest.Context(), &dto.UserRegisterRequest{Email: "other@example.com", Password: "password-1", Code: "123456"}); err == nil {
		t.Fatal("a spent code registered another account")
	}
}

func TestTelephoneRegistrationStoresTheNumberInE164(t *testing.T) {
	f := newFixture(t)
	f.saveCode(t, verification.MobileCodeKey(auth.Register, "+8613800138000"), "123456")

	resp, err := f.svc.TelephoneUserRegister(identitytest.Context(), &dto.TelephoneRegisterRequest{
		TelephoneAreaCode: "86", Telephone: "138 0013 8000", Password: "password-1", Code: "123456",
	})
	if err != nil {
		t.Fatalf("TelephoneUserRegister() error = %v", err)
	}
	_, identities := f.registered(t, resp.Token, "mobile")
	if len(identities) != 1 || identities[0].AuthType != "mobile" || identities[0].AuthIdentifier != "+8613800138000" || !identities[0].Verified {
		t.Fatalf("identities = %+v", identities)
	}
	// The number signs in in any form.
	if _, err := f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: "password-1"}); err != nil {
		t.Fatalf("sign-in with the registered number: %v", err)
	}
}

func TestDeviceLoginRegistersANewDevice(t *testing.T) {
	f := newFixture(t)

	resp, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "device-1"})
	if err != nil {
		t.Fatalf("DeviceLogin() error = %v", err)
	}
	created, identities := f.registered(t, resp.Token, "device")
	if len(identities) != 1 || identities[0].AuthType != "device" || identities[0].AuthIdentifier != "device-1" || !identities[0].Verified {
		t.Fatalf("identities = %+v", identities)
	}
	var device user.Device
	if err := f.DB.Where("identifier = ?", "device-1").First(&device).Error; err != nil {
		t.Fatal(err)
	}
	if device.UserId != created.Id || !device.Enabled || device.Ip != identitytest.ClientIP || device.UserAgent != identitytest.UserAgent {
		t.Fatalf("device = %+v", device)
	}
	claims, err := usersession.Validate(context.Background(), f.Redis, testSecret, resp.Token)
	if err != nil || claims.DeviceID != device.Id || claims.LoginType != "device" {
		t.Fatalf("session claims = %+v, %v; want a device session", claims, err)
	}

	// The same device signs in to the same account.
	again, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "device-1"})
	if err != nil || f.sessionUser(t, again.Token) != created.Id {
		t.Fatalf("second device sign-in = %v", err)
	}
	if n := len(f.Users(t)); n != 1 {
		t.Fatalf("accounts = %d after signing in again, want 1", n)
	}
}

// A registration that fails inside its transaction leaves no account, no
// event and no audit behind.
func TestFailedRegistrationLeavesNothing(t *testing.T) {
	f := newFixture(t)
	if err := f.DB.Exec(`CREATE TRIGGER reject_registration_event BEFORE INSERT ON domain_event_outbox BEGIN SELECT RAISE(FAIL, 'test failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "device-1"}); err == nil {
		t.Fatal("DeviceLogin() succeeded without its registration event")
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

// Once the account is known every attempt is audited, failed or not; an
// unknown identifier leaves no audit.
func TestSignInAuditsEveryAttemptOnAKnownAccount(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "email", "owner@example.com", "password-1")

	_, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "wrong"})
	assertCode(t, err, xerr.UserPasswordError)
	_, err = f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "nobody@example.com", Password: "password-1"})
	assertCode(t, err, xerr.UserNotExist)
	resp, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "OWNER@example.com", Password: "password-1"})
	if err != nil {
		t.Fatalf("UserLogin() error = %v", err)
	}
	if f.sessionUser(t, resp.Token) != owner.Id {
		t.Fatal("the session is not the owner's")
	}
	audits := f.loginAudits(t, owner.Id)
	if len(audits) != 2 || audits[0].Success || !audits[1].Success || audits[1].UserAgent != identitytest.UserAgent {
		t.Fatalf("login audits = %+v, want a failure then a success", audits)
	}
}

// cacheWithoutEnableFlag caches the account under keys the way a cached row
// of an older layout reads: without the enable flag.
func (f *fixture) cacheWithoutEnableFlag(t *testing.T, userID int64, keys ...string) {
	t.Helper()
	loaded, err := f.Store.User().FindOne(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(encoded, &row); err != nil {
		t.Fatal(err)
	}
	delete(row, "Enable")
	if encoded, err = json.Marshal(row); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err := f.Mini.Set(key, string(encoded)); err != nil {
			t.Fatal(err)
		}
	}
}

// An account without an enable flag is refused as disabled rather than
// crashing the sign-in.
func TestSignInRefusesAnAccountWithoutEnableFlag(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "email", "owner@example.com", "password-1")
	phoneOwner := f.account(t, "mobile", "+8613800138000", "password-1")
	f.cacheWithoutEnableFlag(t, owner.Id, "cache:user:id:"+itoa(owner.Id), "cache:user:email:v2:owner@example.com")
	f.cacheWithoutEnableFlag(t, phoneOwner.Id, "cache:user:id:"+itoa(phoneOwner.Id))

	_, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1"})
	assertCode(t, err, xerr.UserDisabled)
	_, err = f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: "password-1"})
	assertCode(t, err, xerr.UserDisabled)
	if audits := f.loginAudits(t, owner.Id); len(audits) != 1 || audits[0].Success {
		t.Fatalf("login audits = %+v, want one failure", audits)
	}
}

func TestTelephoneLoginWithASecurityCode(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "mobile", "+8613800138000", "password-1")
	f.saveCode(t, verification.MobileCodeKey(auth.Security, "+8613800138000"), "654321")

	_, err := f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", TelephoneCode: "000000"})
	assertCode(t, err, xerr.VerifyCodeError)
	resp, err := f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", TelephoneCode: "654321"})
	if err != nil {
		t.Fatalf("TelephoneLogin() error = %v", err)
	}
	if f.sessionUser(t, resp.Token) != owner.Id {
		t.Fatal("the session is not the owner's")
	}
	_, err = f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.InvalidParams)
}

// The administrator's switches close methods and registration.
func TestDisabledMethodsAndClosedRegistrationAreRefused(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "owner@example.com", "password-1")
	f.cfg.EmailEnabled, f.cfg.MobileEnabled, f.cfg.DeviceEnabled = false, false, false

	_, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1"})
	assertCode(t, err, xerr.GetAuthenticatorError)
	_, err = f.svc.ResetPassword(identitytest.Context(), &dto.ResetPasswordRequest{Email: "owner@example.com", Password: "password-2"})
	assertCode(t, err, xerr.GetAuthenticatorError)
	_, err = f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: "x"})
	assertCode(t, err, xerr.GetAuthenticatorError)
	_, err = f.svc.TelephoneResetPassword(identitytest.Context(), &dto.TelephoneResetPasswordRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: "password-2"})
	assertCode(t, err, xerr.GetAuthenticatorError)
	_, err = f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "device-1"})
	assertCode(t, err, xerr.GetAuthenticatorError)

	*f.cfg = openSnapshot()
	f.cfg.StopRegister = true
	_, err = f.svc.UserRegister(identitytest.Context(), &dto.UserRegisterRequest{Email: "new@example.com", Password: "password-1"})
	assertCode(t, err, xerr.StopRegister)
	_, err = f.svc.TelephoneUserRegister(identitytest.Context(), &dto.TelephoneRegisterRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: "password-1"})
	assertCode(t, err, xerr.StopRegister)
	_, err = f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "device-1"})
	assertCode(t, err, xerr.StopRegister)
	if n := len(f.Users(t)); n != 1 {
		t.Fatalf("accounts = %d, want only the existing one", n)
	}
}

// The Turnstile check the handlers used to run is the service's, keyed by
// the purpose's switch and the request's client address.
func TestTurnstileGuardsSignInResetAndRegistration(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "owner@example.com", "password-1")
	var remoteIPs []string
	f.svc = NewService(Deps{Store: f.Store, Redis: f.Redis, Config: func() Snapshot { return *f.cfg },
		VerifyTurnstile: func(_ context.Context, _, token, ip string) (bool, error) {
			remoteIPs = append(remoteIPs, ip)
			return token == "human", nil
		}})
	f.cfg.TurnstileSecret = "site-secret"
	f.cfg.LoginVerify, f.cfg.ResetPasswordVerify, f.cfg.RegisterVerify = true, true, true

	_, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1", CfToken: "robot"})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = f.svc.ResetPassword(identitytest.Context(), &dto.ResetPasswordRequest{Email: "owner@example.com", Password: "password-2"})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = f.svc.UserRegister(identitytest.Context(), &dto.UserRegisterRequest{Email: "new@example.com", Password: "password-1", CfToken: "robot"})
	assertCode(t, err, xerr.TooManyRequests)
	if _, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1", CfToken: "human"}); err != nil {
		t.Fatalf("a verified sign-in was refused: %v", err)
	}
	for _, ip := range remoteIPs {
		if ip != identitytest.ClientIP {
			t.Fatalf("Turnstile checked address %q, want the request's %q", ip, identitytest.ClientIP)
		}
	}
}

func TestCheckUserFindsNormalizedIdentities(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "owner@example.com", "password-1")
	f.account(t, "mobile", "+8613800138000", "password-1")

	for email, want := range map[string]bool{"owner@example.com": true, "OWNER@example.com": true, "nobody@example.com": false} {
		resp, err := f.svc.CheckUser(context.Background(), &dto.CheckUserRequest{Email: email})
		if err != nil || resp.Exist != want {
			t.Fatalf("CheckUser(%q) = %+v, %v; want %v", email, resp, err, want)
		}
	}
	resp, err := f.svc.CheckUserTelephone(context.Background(), &dto.TelephoneCheckUserRequest{TelephoneAreaCode: "86", Telephone: "138-0013-8000"})
	if err != nil || !resp.Exist {
		t.Fatalf("CheckUserTelephone = %+v, %v", resp, err)
	}
	_, err = f.svc.CheckUserTelephone(context.Background(), &dto.TelephoneCheckUserRequest{TelephoneAreaCode: "86", Telephone: "abc"})
	assertCode(t, err, xerr.TelephoneError)
}

// An identifier is not proof of ownership: a sign-in naming another
// account's device is refused and changes nothing.
func TestSignInCannotTakeAnotherAccountsDevice(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "owner@example.com", "password-1")
	if _, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "device-1"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1", Identifier: "device-1"})
	assertCode(t, err, xerr.InvalidAccess)
	var device user.Device
	if err := f.DB.Where("identifier = ?", "device-1").First(&device).Error; err != nil {
		t.Fatal(err)
	}
	if device.UserId == 1 {
		t.Fatal("the device moved to the signing-in account")
	}
}
