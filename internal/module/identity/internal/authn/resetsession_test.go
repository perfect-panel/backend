package authn

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// A reset usually follows a compromise, so sessions issued before it end
// while the one it issues works.
func TestResetPasswordRevokesEarlierSessions(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "email", "owner@example.com", "old-password")
	earlier, err := usersession.Issue(context.Background(), f.Redis, testSecret, 3600, usersession.Grant{UserID: owner.Id, LoginType: "email"})
	if err != nil {
		t.Fatal(err)
	}
	f.saveCode(t, verification.EmailCodeKey(auth.Security, "owner@example.com"), "123456")

	resp, err := f.svc.ResetPassword(identitytest.Context(), &dto.ResetPasswordRequest{Email: "Owner@example.com", Code: "123456", Password: "new-password-1"})
	if err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	var stored user.User
	if err := f.DB.First(&stored, owner.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !password.MultiPasswordVerify(stored.Algo, stored.Salt, "new-password-1", stored.Password) {
		t.Fatal("the new password was not written")
	}
	if _, err := usersession.Validate(context.Background(), f.Redis, testSecret, earlier); err == nil {
		t.Fatal("a session from before the reset still works")
	}
	if f.sessionUser(t, resp.Token) != owner.Id {
		t.Fatal("the session issued by the reset does not work")
	}
	// The reset itself is audited, then the sign-in it ends with.
	audits := f.loginAudits(t, owner.Id)
	if len(audits) != 2 || audits[0].Method != account.PasswordReset || !audits[0].Success || audits[0].LoginIP != identitytest.ClientIP ||
		!audits[1].Success || audits[1].Method != "email" {
		t.Fatalf("login audits = %+v, want the reset then its sign-in", audits)
	}
	if len(resp.ThirdPartyBindings) != 0 {
		t.Fatalf("third-party bindings = %v, want none for an email-only account", resp.ThirdPartyBindings)
	}
}

// A reset reports the third-party sign-in methods it leaves bound, so the
// account can revisit a binding made during a compromise, and tells the
// account about the change.
func TestResetPasswordReportsTheThirdPartyBindings(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "email", "owner@example.com", "old-password")
	for _, binding := range []user.AuthMethods{
		{UserId: owner.Id, AuthType: "telegram", AuthIdentifier: "10001", Verified: true},
		{UserId: owner.Id, AuthType: "github", AuthIdentifier: "583231", Verified: true},
		{UserId: owner.Id, AuthType: "device", AuthIdentifier: "device-1", Verified: true},
	} {
		if err := f.DB.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	var notified struct {
		userID   int64
		bindings []string
	}
	f.svc.deps.NotifyPasswordChanged = func(_ context.Context, userID int64, bindings []string) error {
		notified.userID, notified.bindings = userID, bindings
		return errors.New("bot unavailable")
	}
	f.saveCode(t, verification.EmailCodeKey(auth.Security, "owner@example.com"), "123456")

	resp, err := f.svc.ResetPassword(identitytest.Context(), &dto.ResetPasswordRequest{Email: "owner@example.com", Code: "123456", Password: "new-password-1"})
	if err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	want := []string{"github", "telegram"}
	if !slices.Equal(resp.ThirdPartyBindings, want) {
		t.Fatalf("third-party bindings = %v, want %v (device and email are the account's own)", resp.ThirdPartyBindings, want)
	}
	if notified.userID != owner.Id || !slices.Equal(notified.bindings, want) {
		t.Fatalf("notified %d of %v, want %d of %v", notified.userID, notified.bindings, owner.Id, want)
	}
}

// A code sent to an identifier a deleted or disabled account still holds
// must not bring the account back. The refused attempt is audited like a
// refused sign-in.
func TestResetPasswordRejectsDeletedAndDisabledAccounts(t *testing.T) {
	for name, disable := range map[string]string{
		"deleted":  "UPDATE user SET deleted_at = CURRENT_TIMESTAMP",
		"disabled": "UPDATE user SET enable = false",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			owner := f.account(t, "mobile", "+8613800138000", "old-password")
			if err := f.DB.Exec(disable).Error; err != nil {
				t.Fatal(err)
			}
			f.saveCode(t, verification.MobileCodeKey(auth.Security, "+8613800138000"), "123456")

			_, err := f.svc.TelephoneResetPassword(identitytest.Context(), &dto.TelephoneResetPasswordRequest{
				TelephoneAreaCode: "86", Telephone: "13800138000", Code: "123456", Password: "new-password-1",
			})
			if code := xerr.CodeOf(err); err == nil || (code != xerr.UserNotExist && code != xerr.UserDisabled) {
				t.Fatalf("error = %v, want the account refused", err)
			}
			var stored user.User
			if err := f.DB.Unscoped().First(&stored, owner.Id).Error; err != nil {
				t.Fatal(err)
			}
			if !password.MultiPasswordVerify(stored.Algo, stored.Salt, "old-password", stored.Password) {
				t.Fatal("the account's password was written")
			}
			if audits := f.loginAudits(t, owner.Id); len(audits) != 1 || audits[0].Success {
				t.Fatalf("login audits = %+v, want one failure", audits)
			}
		})
	}
}

func TestResetPasswordNeedsTheCodeBeforeLookingTheAccountUp(t *testing.T) {
	f := newFixture(t)
	owner := f.account(t, "email", "owner@example.com", "old-password")
	_, err := f.svc.ResetPassword(identitytest.Context(), &dto.ResetPasswordRequest{Email: "owner@example.com", Code: "000000", Password: "new-password-1"})
	assertCode(t, err, xerr.VerifyCodeError)
	_, err = f.svc.ResetPassword(identitytest.Context(), &dto.ResetPasswordRequest{Email: "nobody@example.com", Code: "000000", Password: "new-password-1"})
	assertCode(t, err, xerr.VerifyCodeError)
	if audits := f.loginAudits(t, owner.Id); len(audits) != 0 {
		t.Fatalf("login audits = %+v, want none before the code is proven", audits)
	}
}
