package profile

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	usermodel "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func (f *rebindFixture) bindMobile(u *usermodel.User, req *dto.UpdateBindMobileRequest) error {
	return f.svc.UpdateBindMobile(usermodel.NewContext(context.Background(), u), req)
}

// A first mobile binding needs the code sent to the number, stored in E.164;
// replacing it needs the current password too and ends every session.
func TestUpdateBindMobileProvesTheNumberAndTheReplacement(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "password-1")
	f.code(t, verification.MobileCodeKey(auth.Register, "+8613800138000"), "123456")
	first := &dto.UpdateBindMobileRequest{AreaCode: "86", Mobile: "138 0013 8000", Code: "123456"}

	assertCode(t, f.bindMobile(owner, &dto.UpdateBindMobileRequest{AreaCode: "86", Mobile: "13800138000", Code: "000000"}), xerr.VerifyCodeError)
	if err := f.bindMobile(owner, first); err != nil {
		t.Fatalf("first binding: error = %v", err)
	}
	bound := f.identity(t, owner, "mobile")
	if bound == nil || !bound.Verified || bound.AuthIdentifier != "+8613800138000" {
		t.Fatalf("binding = %+v, want a verified binding of +8613800138000", bound)
	}

	session := f.session(t, owner)
	f.code(t, verification.MobileCodeKey(auth.Register, "+8613900139000"), "654321")
	replace := func(plain string) *dto.UpdateBindMobileRequest {
		return &dto.UpdateBindMobileRequest{AreaCode: "86", Mobile: "13900139000", Code: "654321", Password: plain}
	}
	assertCode(t, f.bindMobile(owner, replace("")), xerr.InvalidParams)
	assertCode(t, f.bindMobile(owner, replace("guess")), xerr.UserPasswordError)
	if attempts, _ := f.Mini.Get(account.PasswordAttemptKey(owner.Id)); attempts != "1" {
		t.Fatalf("attempts = %q, want the wrong password counted against the lockout", attempts)
	}
	if bound := f.identity(t, owner, "mobile"); bound.AuthIdentifier != "+8613800138000" || !f.sessionLive(session) {
		t.Fatalf("binding = %+v, session live = %v; want nothing changed by the refused replacement", bound, f.sessionLive(session))
	}

	if err := f.bindMobile(owner, replace("password-1")); err != nil {
		t.Fatalf("replacement: error = %v", err)
	}
	if bound := f.identity(t, owner, "mobile"); bound == nil || bound.AuthIdentifier != "+8613900139000" || len(f.Identities(t, owner.Id)) != 1 {
		t.Fatalf("identities = %+v, want the one mobile binding replaced", f.Identities(t, owner.Id))
	}
	if f.sessionLive(session) {
		t.Fatal("a session from before the replacement still works")
	}
}

// An account without a password replaces its number with a security code
// sent to the current number.
func TestReplacingTheBoundMobileWithoutAPasswordNeedsTheCurrentNumbersCode(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "")
	f.bind(t, owner, "mobile", "+8613800138000")
	f.code(t, verification.MobileCodeKey(auth.Register, "+8613900139000"), "654321")
	f.code(t, verification.MobileCodeKey(auth.Security, "+8613800138000"), "111111")
	request := func(current string) *dto.UpdateBindMobileRequest {
		return &dto.UpdateBindMobileRequest{AreaCode: "86", Mobile: "13900139000", Code: "654321", CurrentCode: current}
	}

	assertCode(t, f.bindMobile(owner, request("")), xerr.InvalidParams)
	assertCode(t, f.bindMobile(owner, request("000000")), xerr.VerifyCodeError)
	if err := f.bindMobile(owner, request("111111")); err != nil {
		t.Fatalf("replacement: error = %v", err)
	}
	if bound := f.identity(t, owner, "mobile"); bound == nil || bound.AuthIdentifier != "+8613900139000" {
		t.Fatalf("binding = %+v, want +8613900139000", bound)
	}
}
