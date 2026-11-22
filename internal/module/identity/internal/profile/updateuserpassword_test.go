package profile

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	usermodel "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// passwordUsers records the columns the flow writes.
type passwordUsers struct {
	written map[string]any
}

var _ Users = (*passwordUsers)(nil)

func (r *passwordUsers) UpdateColumns(_ context.Context, _ int64, columns map[string]any) error {
	r.written = columns
	return nil
}

// newPasswordService returns the service, its account rows and Redis, and
// the context of the signed-in account current.
func newPasswordService(t *testing.T, current *usermodel.User) (*Service, *passwordUsers, *miniredis.Miniredis, *redis.Client, context.Context) {
	t.Helper()
	logtest.Discard(t)
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	users := &passwordUsers{}
	ctx := usermodel.NewContext(context.Background(), current)
	return NewService(Deps{Users: users, Redis: rds}), users, server, rds, ctx
}

// A session alone must not be enough to change the password: that would turn
// a stolen session into the account for good.
func TestUpdateUserPasswordRequiresCurrentPasswordAndEndsSessions(t *testing.T) {
	current := &usermodel.User{Id: 7, Password: password.EncodePassWord("old-password"), Algo: password.PasswordAlgoArgon2id}
	svc, users, _, rds, ctx := newPasswordService(t, current)
	before, err := usersession.AcquireEpoch(context.Background(), rds, 7)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateUserPassword(ctx, &dto.UpdateUserPasswordRequest{OldPassword: "guessed", Password: "new-password-1"}); err == nil || users.written != nil {
		t.Fatalf("wrong current password: error = %v, written = %v", err, users.written)
	}

	if _, err := svc.UpdateUserPassword(ctx, &dto.UpdateUserPasswordRequest{OldPassword: "old-password", Password: "new-password-1"}); err != nil {
		t.Fatalf("UpdateUserPassword() error = %v", err)
	}
	if hash, _ := users.written["password"].(string); !password.VerifyPassWord("new-password-1", hash) {
		t.Fatal("new password was not written")
	}
	after, _ := rds.Get(context.Background(), usersession.Key(7)).Result()
	if usersession.Check(map[string]any{usersession.EpochClaim: before}, after) == nil {
		t.Fatal("sessions from before the change still work")
	}
}

// Guesses at the current password through a session count against the same
// limit as sign-in: after the window's attempts even the right password is
// refused until the window ends, and the right one clears the count.
func TestUpdateUserPasswordGuessesAreCappedLikeSignIn(t *testing.T) {
	current := &usermodel.User{Id: 7, Password: password.EncodePassWord("old-password"), Algo: password.PasswordAlgoArgon2id}
	svc, users, server, _, ctx := newPasswordService(t, current)
	request := func(old string) *dto.UpdateUserPasswordRequest {
		return &dto.UpdateUserPasswordRequest{OldPassword: old, Password: "new-password-1"}
	}

	change := func(old string) error {
		_, err := svc.UpdateUserPassword(ctx, request(old))
		return err
	}
	for i := 0; i < account.MaxPasswordAttempts; i++ {
		assertCode(t, change("guess"), xerr.UserPasswordError)
	}
	assertCode(t, change("old-password"), xerr.TooManyRequests)
	if users.written != nil {
		t.Fatalf("written = %v, want no password written while locked out", users.written)
	}

	server.FastForward(account.PasswordAttemptWindow)
	if err := change("old-password"); err != nil {
		t.Fatalf("UpdateUserPassword() after the window: error = %v", err)
	}
	if users.written == nil {
		t.Fatal("the new password was not written")
	}
	if server.Exists(account.PasswordAttemptKey(7)) {
		t.Fatal("the right password did not clear the attempts")
	}
}

// storedPassword returns the account's password columns as stored.
func (f *rebindFixture) storedPassword(t *testing.T, u *usermodel.User) (hash, algo, salt string) {
	t.Helper()
	var stored usermodel.User
	if err := f.DB.First(&stored, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	return stored.Password, stored.Algo, stored.Salt
}

// setPassword sets the first password of the passwordless account u.
func (f *rebindFixture) setPassword(u *usermodel.User, currentCode string) error {
	_, err := f.svc.UpdateUserPassword(usermodel.NewContext(context.Background(), u), &dto.UpdateUserPasswordRequest{Password: "first-password", CurrentCode: currentCode})
	return err
}

// A stolen session on a passwordless account must not bootstrap a password
// that then proves replacing the bound address: setting the first password
// of an account with a bound email or phone number needs the security code
// sent to one of them. The code is spent with the change, and every session
// ends.
func TestFirstPasswordNeedsTheCodeSentToABoundAddress(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "")
	f.bind(t, owner, "email", "owner@example.com")
	f.bind(t, owner, "mobile", "+8613800138000")
	session := f.session(t, owner)
	emailKey := verification.EmailCodeKey(auth.Security, "owner@example.com")
	mobileKey := verification.MobileCodeKey(auth.Security, "+8613800138000")
	f.code(t, emailKey, "123456")
	f.code(t, mobileKey, "654321")

	assertCode(t, f.setPassword(owner, ""), xerr.InvalidParams)
	assertCode(t, f.setPassword(owner, "000000"), xerr.VerifyCodeError)
	if hash, _, _ := f.storedPassword(t, owner); hash != "" {
		t.Fatal("a password was set without the code")
	}
	if !f.sessionLive(session) {
		t.Fatal("a refused change ended the session")
	}

	// The code of either bound address proves the account.
	if err := f.setPassword(owner, "654321"); err != nil {
		t.Fatalf("UpdateUserPassword() with the phone number's code: error = %v", err)
	}
	hash, algo, salt := f.storedPassword(t, owner)
	if !password.MultiPasswordVerify(algo, salt, "first-password", hash) {
		t.Fatal("the first password was not written")
	}
	if f.Mini.Exists(mobileKey) || !f.Mini.Exists(emailKey) {
		t.Fatal("the code that proved the account was not the one spent")
	}
	if f.sessionLive(session) {
		t.Fatal("a session from before the change still works")
	}

	// The account now has a password, which the next request, whose context
	// carries the account as the auth middleware reloads it, must prove: a
	// remaining code no longer changes the password.
	var reloaded usermodel.User
	if err := f.DB.First(&reloaded, owner.Id).Error; err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.UpdateUserPassword(usermodel.NewContext(context.Background(), &reloaded), &dto.UpdateUserPasswordRequest{Password: "second-password", CurrentCode: "123456"})
	assertCode(t, err, xerr.UserPasswordError)
}

// An account that signs in only through a provider or a device has neither
// a password nor an address a code could go to, so its session sets the
// first password.
func TestOAuthOnlyAccountSetsItsFirstPasswordWithTheSession(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "")
	f.bind(t, owner, "telegram", "42")

	if err := f.setPassword(owner, ""); err != nil {
		t.Fatalf("UpdateUserPassword() error = %v", err)
	}
	hash, algo, salt := f.storedPassword(t, owner)
	if !password.MultiPasswordVerify(algo, salt, "first-password", hash) {
		t.Fatal("the first password was not written")
	}
}

func TestLogoutEndsOnlyTheCallingSession(t *testing.T) {
	rds := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	for _, id := range []string{"current", "other"} {
		if err := rds.Set(context.Background(), usersession.SessionKey(id), 7, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(context.Background(), requestctx.CtxKeySessionID, "current")

	if err := NewService(Deps{Redis: rds}).Logout(ctx); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	if rds.Exists(context.Background(), usersession.SessionKey("current")).Val() != 0 {
		t.Fatal("the calling session still exists")
	}
	if rds.Exists(context.Background(), usersession.SessionKey("other")).Val() != 1 {
		t.Fatal("another session was ended")
	}
}
