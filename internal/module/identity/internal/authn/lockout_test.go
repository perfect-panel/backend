package authn

import (
	"context"
	"sync"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The password guess limit holds against a burst: of many concurrent wrong
// passwords only the window's attempts reach the password check, the rest
// are refused before it, and the account stays locked for the window even
// to the right password. The limit is proven by the outcomes: only an
// attempt that reached the check reports a wrong password.
func TestConcurrentWrongPasswordsCannotExceedTheLockout(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "owner@example.com", "password-1")
	const burst = 6 * account.MaxPasswordAttempts

	outcomes := make([]error, burst)
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, outcomes[i] = f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "wrong"})
		}(i)
	}
	wg.Wait()

	var checked, refused int
	for _, err := range outcomes {
		switch xerr.CodeOf(err) {
		case xerr.UserPasswordError:
			checked++
		case xerr.TooManyRequests:
			refused++
		default:
			t.Fatalf("unexpected outcome: %v", err)
		}
	}
	if checked != account.MaxPasswordAttempts || refused != burst-account.MaxPasswordAttempts {
		t.Fatalf("%d guesses reached the password check and %d were refused, want %d and %d", checked, refused, account.MaxPasswordAttempts, burst-account.MaxPasswordAttempts)
	}

	_, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1"})
	assertCode(t, err, xerr.TooManyRequests)

	f.Mini.FastForward(account.PasswordAttemptWindow)
	if _, err := f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1"}); err != nil {
		t.Fatalf("sign-in after the window: %v", err)
	}
	if f.Mini.Exists(account.PasswordAttemptKey(1)) {
		t.Fatal("a successful sign-in did not clear the attempts")
	}
}

// A wrong password counts against the same limit however the account signs
// in, and the right one clears the count.
func TestTelephonePasswordGuessesShareTheLockout(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "mobile", "+8613800138000", "password-1")
	request := func(plain string) *dto.TelephoneLoginRequest {
		return &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: plain}
	}
	for i := 0; i < account.MaxPasswordAttempts; i++ {
		_, err := f.svc.TelephoneLogin(identitytest.Context(), request("wrong"))
		assertCode(t, err, xerr.UserPasswordError)
	}
	_, err := f.svc.TelephoneLogin(identitytest.Context(), request("password-1"))
	assertCode(t, err, xerr.TooManyRequests)

	f.Mini.FastForward(account.PasswordAttemptWindow)
	if _, err := f.svc.TelephoneLogin(identitytest.Context(), request("password-1")); err != nil {
		t.Fatalf("sign-in after the window: %v", err)
	}
	if f.Mini.Exists(account.PasswordAttemptKey(owner.Id)) {
		t.Fatal("a successful sign-in did not clear the attempts")
	}
}

// revokeDuringPasswordCheck makes every password check revoke the account's
// sessions before it compares, as a reset landing in the Argon2 queue does.
func (f *fixture) revokeDuringPasswordCheck(t *testing.T, userID int64) {
	t.Helper()
	verify := verifyPassword
	t.Cleanup(func() { verifyPassword = verify })
	verifyPassword = func(algo, salt, plain, hash string) bool {
		if err := usersession.Revoke(context.Background(), f.Redis, userID); err != nil {
			t.Error(err)
		}
		return verify(algo, salt, plain, hash)
	}
}

// A password reset that lands while a sign-in checks the password ends that
// sign-in too: it gets no session, rather than one carrying the epoch the
// reset just set.
func TestSignInOvertakenByARevocationGetsNoSession(t *testing.T) {
	for name, signIn := range map[string]func(t *testing.T, f *fixture) (*dto.LoginResponse, error){
		"email": func(t *testing.T, f *fixture) (*dto.LoginResponse, error) {
			f.account(t, "email", "owner@example.com", "password-1")
			f.revokeDuringPasswordCheck(t, 1)
			return f.svc.UserLogin(identitytest.Context(), &dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1"})
		},
		"telephone": func(t *testing.T, f *fixture) (*dto.LoginResponse, error) {
			f.account(t, "mobile", "+8613800138000", "password-1")
			f.revokeDuringPasswordCheck(t, 1)
			return f.svc.TelephoneLogin(identitytest.Context(), &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: "password-1"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			resp, err := signIn(t, f)
			if err == nil {
				if _, validateErr := usersession.Validate(context.Background(), f.Redis, testSecret, resp.Token); validateErr == nil {
					t.Fatal("the sign-in the reset overtook holds a live session")
				}
				t.Fatal("the sign-in the reset overtook succeeded")
			}
			assertCode(t, err, xerr.InvalidAccess)
			if audits := f.loginAudits(t, 1); len(audits) != 1 || audits[0].Success {
				t.Fatalf("login audits = %+v, want one failure", audits)
			}
		})
	}
}
