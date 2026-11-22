package authn

import (
	"fmt"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The existence checks answer a client address a bounded number of times a
// minute, whatever it asks about, so accounts cannot be enumerated at line
// speed; another address keeps its own quota, and the quota returns with
// the next minute.
func TestExistenceChecksAreRateLimitedPerClientAddress(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "owner@example.com", "password-1")
	prober := fromIP("198.51.100.7")

	for i := 0; i < ExistenceChecksPerMinute; i++ {
		var err error
		if i%2 == 0 {
			_, err = f.svc.CheckUser(prober, &dto.CheckUserRequest{Email: fmt.Sprintf("probe-%d@example.com", i)})
		} else {
			_, err = f.svc.CheckUserTelephone(prober, &dto.TelephoneCheckUserRequest{TelephoneAreaCode: "86", Telephone: fmt.Sprintf("1380013%04d", i)})
		}
		if err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
	}
	_, err := f.svc.CheckUser(prober, &dto.CheckUserRequest{Email: "owner@example.com"})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = f.svc.CheckUserTelephone(prober, &dto.TelephoneCheckUserRequest{TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.TooManyRequests)

	resp, err := f.svc.CheckUser(fromIP("203.0.113.9"), &dto.CheckUserRequest{Email: "owner@example.com"})
	if err != nil || !resp.Exist {
		t.Fatalf("another address's check = %+v, %v", resp, err)
	}
	f.Mini.FastForward(61 * 1e9)
	if _, err := f.svc.CheckUser(prober, &dto.CheckUserRequest{Email: "owner@example.com"}); err != nil {
		t.Fatalf("check in the next minute: %v", err)
	}
}

// Whether a phone number's account is disabled or deleted is revealed only
// to whoever holds its credential, as with email sign-in: a wrong password
// or code gets the credential's refusal, not the account's state.
func TestTelephoneLoginRevealsTheAccountStateOnlyToTheCredentialHolder(t *testing.T) {
	for name, disable := range map[string]string{
		"deleted":  "UPDATE user SET deleted_at = CURRENT_TIMESTAMP",
		"disabled": "UPDATE user SET enable = false",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.account(t, "mobile", "+8613800138000", "password-1")
			if err := f.DB.Exec(disable).Error; err != nil {
				t.Fatal(err)
			}
			request := func(plain, code string) *dto.TelephoneLoginRequest {
				return &dto.TelephoneLoginRequest{TelephoneAreaCode: "86", Telephone: "13800138000", Password: plain, TelephoneCode: code}
			}
			_, err := f.svc.TelephoneLogin(identitytest.Context(), request("wrong", ""))
			assertCode(t, err, xerr.UserPasswordError)
			_, err = f.svc.TelephoneLogin(identitytest.Context(), request("", "000000"))
			assertCode(t, err, xerr.VerifyCodeError)
			_, err = f.svc.TelephoneLogin(identitytest.Context(), request("", ""))
			assertCode(t, err, xerr.InvalidParams)

			// The owner learns the state.
			_, err = f.svc.TelephoneLogin(identitytest.Context(), request("password-1", ""))
			if code := xerr.CodeOf(err); code != xerr.UserDisabled && code != xerr.UserNotExist {
				t.Fatalf("the owner's sign-in: error = %v, want the account state", err)
			}
		})
	}
}
