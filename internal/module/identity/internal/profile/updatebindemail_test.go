package profile

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	usermodel "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const rebindTestSecret = "rebind-test-secret"

// rebindFixture is the profile service over the module's test store with
// email and mobile sign-in enabled.
type rebindFixture struct {
	*identitytest.Env
	svc *Service
}

func newRebindFixture(t *testing.T) *rebindFixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	svc := NewService(Deps{
		Users:     env.Store.User(),
		UserAuth:  env.Store.UserAuth(),
		UserCache: env.Store.UserCache(),
		Redis:     env.Redis,
		Store:     env.Store,
		Policy: registerpolicy.New(registerpolicy.Deps{Config: func() registerpolicy.Snapshot {
			return registerpolicy.Snapshot{EmailEnabled: true, MobileEnabled: true}
		}}),
		EmailDomains: func() (string, bool) { return "", false },
	})
	return &rebindFixture{Env: env, svc: svc}
}

// account creates an enabled account; plain is its password, none when
// empty, as for an account created through OAuth or a device.
func (f *rebindFixture) account(t *testing.T, plain string) *usermodel.User {
	t.Helper()
	enabled := true
	u := &usermodel.User{Enable: &enabled}
	if plain != "" {
		u.Password, u.Algo = password.EncodePassWord(plain), password.PasswordAlgoArgon2id
	}
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *rebindFixture) bind(t *testing.T, u *usermodel.User, authType, identifier string) {
	t.Helper()
	if err := f.DB.Create(&usermodel.AuthMethods{UserId: u.Id, AuthType: authType, AuthIdentifier: identifier, Verified: true}).Error; err != nil {
		t.Fatal(err)
	}
}

func (f *rebindFixture) code(t *testing.T, key, code string) {
	t.Helper()
	if err := verification.SaveVerificationCode(context.Background(), f.Redis, key, code, time.Minute); err != nil {
		t.Fatal(err)
	}
}

// session issues a session of the account, which a replacement must end.
func (f *rebindFixture) session(t *testing.T, u *usermodel.User) string {
	t.Helper()
	token, err := usersession.Issue(context.Background(), f.Redis, rebindTestSecret, 3600, usersession.Grant{UserID: u.Id})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (f *rebindFixture) sessionLive(token string) bool {
	_, err := usersession.Validate(context.Background(), f.Redis, rebindTestSecret, token)
	return err == nil
}

// identity returns the account's binding of authType, or nil.
func (f *rebindFixture) identity(t *testing.T, u *usermodel.User, authType string) *usermodel.AuthMethods {
	t.Helper()
	for _, m := range f.Identities(t, u.Id) {
		if m.AuthType == authType {
			return &m
		}
	}
	return nil
}

func (f *rebindFixture) bindEmail(u *usermodel.User, req *dto.UpdateBindEmailRequest) error {
	return f.svc.UpdateBindEmail(usermodel.NewContext(context.Background(), u), req)
}

// The bound email becomes a login and password-reset identifier, so the
// caller must prove control of the address; a session alone is not enough.
// A first binding needs nothing else.
func TestUpdateBindEmailRequiresCodeSentToNewAddress(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "password-1")
	session := f.session(t, owner)
	f.code(t, verification.EmailCodeKey(auth.Register, "new@example.com"), "123456")

	err := f.bindEmail(owner, &dto.UpdateBindEmailRequest{Email: "new@example.com", Code: "000000"})
	assertCode(t, err, xerr.VerifyCodeError)
	if f.identity(t, owner, "email") != nil {
		t.Fatal("a wrong code bound the address")
	}

	if err := f.bindEmail(owner, &dto.UpdateBindEmailRequest{Email: "New@Example.com", Code: "123456"}); err != nil {
		t.Fatalf("correct code: error = %v", err)
	}
	bound := f.identity(t, owner, "email")
	if bound == nil || !bound.Verified || bound.AuthIdentifier != "new@example.com" {
		t.Fatalf("binding = %+v, want a verified binding of new@example.com", bound)
	}
	if !f.sessionLive(session) {
		t.Fatal("a first binding ended the caller's session")
	}
	// The code is single use.
	if err := f.bindEmail(owner, &dto.UpdateBindEmailRequest{Email: "other@example.com", Code: "123456"}); err == nil {
		t.Fatal("a consumed code bound another address")
	}
}

// Replacing the bound email of an account with a password needs that
// password: a stolen session cannot move the account to the thief's address
// and reset the password from there. Guesses count against the sign-in
// lockout, and the replacement ends every session.
func TestReplacingTheBoundEmailNeedsTheCurrentPassword(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "password-1")
	f.bind(t, owner, "email", "owner@example.com")
	session := f.session(t, owner)
	newCode := verification.EmailCodeKey(auth.Register, "thief@example.com")
	f.code(t, newCode, "123456")
	request := func(plain string) *dto.UpdateBindEmailRequest {
		return &dto.UpdateBindEmailRequest{Email: "thief@example.com", Code: "123456", Password: plain}
	}

	assertCode(t, f.bindEmail(owner, request("")), xerr.InvalidParams)
	for i := 0; i < account.MaxPasswordAttempts; i++ {
		assertCode(t, f.bindEmail(owner, request("guess")), xerr.UserPasswordError)
	}
	assertCode(t, f.bindEmail(owner, request("password-1")), xerr.TooManyRequests)
	if bound := f.identity(t, owner, "email"); bound.AuthIdentifier != "owner@example.com" {
		t.Fatalf("binding = %+v, want the address unchanged", bound)
	}
	if !f.sessionLive(session) {
		t.Fatal("a refused replacement ended the session")
	}
	if f.Mini.Exists(newCode) != true {
		t.Fatal("a refused replacement spent the new address's code")
	}

	// The window passes, and with it the code's lifetime: a fresh code is sent.
	f.Mini.FastForward(account.PasswordAttemptWindow)
	f.code(t, newCode, "123456")
	if err := f.bindEmail(owner, request("password-1")); err != nil {
		t.Fatalf("replacement with the right password: error = %v", err)
	}
	bound := f.identity(t, owner, "email")
	if bound == nil || !bound.Verified || bound.AuthIdentifier != "thief@example.com" || len(f.Identities(t, owner.Id)) != 1 {
		t.Fatalf("identities = %+v, want the one email binding replaced", f.Identities(t, owner.Id))
	}
	if f.sessionLive(session) {
		t.Fatal("a session from before the replacement still works")
	}
	if f.Mini.Exists(account.PasswordAttemptKey(owner.Id)) {
		t.Fatal("the right password did not clear the attempts")
	}
}

// An account without a password proves the replacement with a security code
// sent to the address being replaced, which is spent with the replacement.
func TestReplacingTheBoundEmailWithoutAPasswordNeedsTheCurrentAddressCode(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "")
	f.bind(t, owner, "email", "owner@example.com")
	session := f.session(t, owner)
	currentCode := verification.EmailCodeKey(auth.Security, "owner@example.com")
	f.code(t, verification.EmailCodeKey(auth.Register, "new@example.com"), "123456")
	f.code(t, currentCode, "654321")
	request := func(current string) *dto.UpdateBindEmailRequest {
		return &dto.UpdateBindEmailRequest{Email: "new@example.com", Code: "123456", CurrentCode: current}
	}

	assertCode(t, f.bindEmail(owner, request("")), xerr.InvalidParams)
	assertCode(t, f.bindEmail(owner, request("000000")), xerr.VerifyCodeError)
	if bound := f.identity(t, owner, "email"); bound.AuthIdentifier != "owner@example.com" {
		t.Fatalf("binding = %+v, want the address unchanged", bound)
	}

	if err := f.bindEmail(owner, request("654321")); err != nil {
		t.Fatalf("replacement with the current address's code: error = %v", err)
	}
	if bound := f.identity(t, owner, "email"); bound == nil || bound.AuthIdentifier != "new@example.com" || !bound.Verified {
		t.Fatalf("binding = %+v, want new@example.com", bound)
	}
	if f.Mini.Exists(currentCode) {
		t.Fatal("the current address's code was not spent")
	}
	if f.sessionLive(session) {
		t.Fatal("a session from before the replacement still works")
	}
}

// An address another account holds cannot be bound, whatever the proof.
func TestUpdateBindEmailRefusesAnotherAccountsAddress(t *testing.T) {
	f := newRebindFixture(t)
	owner, other := f.account(t, "password-1"), f.account(t, "")
	f.bind(t, other, "email", "taken@example.com")
	f.code(t, verification.EmailCodeKey(auth.Register, "taken@example.com"), "123456")

	err := f.bindEmail(owner, &dto.UpdateBindEmailRequest{Email: "taken@example.com", Code: "123456", Password: "password-1"})
	assertCode(t, err, xerr.UserExist)
	if f.identity(t, owner, "email") != nil {
		t.Fatal("the taken address was bound")
	}
}
