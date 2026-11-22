package verifycode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func TestEmailCodeIsSentAndChecked(t *testing.T) {
	f := newCodeFixture(t)
	if _, err := f.svc.SendEmailCode(identitytest.Context(), &dto.SendCodeRequest{Email: "New@Example.com", Type: uint8(auth.Register)}); err != nil {
		t.Fatalf("SendEmailCode() error = %v", err)
	}
	if len(f.queue.tasks) != 1 || f.queue.tasks[0].Type() != taskqueue.ForthwithSendEmail {
		t.Fatalf("tasks = %+v", f.queue.tasks)
	}
	var payload taskqueue.SendEmailPayload
	if err := json.Unmarshal(f.queue.tasks[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	code, _ := payload.Content["Code"].(string)
	if payload.Email != "new@example.com" || code == "" || payload.UserAgent != identitytest.UserAgent {
		t.Fatalf("payload = %+v", payload)
	}
	resp, err := f.svc.CheckVerificationCode(context.Background(), &dto.CheckVerificationCodeRequest{
		Method: "email", Account: "NEW@example.com", Code: code, Type: uint8(auth.Register),
	})
	if err != nil || !resp.Status {
		t.Fatalf("CheckVerificationCode = %+v, %v; want the code found", resp, err)
	}
}

func TestEmailCodePurposeFollowsTheBinding(t *testing.T) {
	f := newCodeFixture(t)
	f.bind(t, "email", "owner@example.com")

	_, err := f.svc.SendEmailCode(identitytest.Context(), &dto.SendCodeRequest{Email: "owner@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.UserExist)
	_, err = f.svc.SendEmailCode(identitytest.Context(), &dto.SendCodeRequest{Email: "nobody@example.com", Type: uint8(auth.Security)})
	assertCode(t, err, xerr.UserNotExist)
	f.policy.StopRegister = true
	_, err = f.svc.SendEmailCode(identitytest.Context(), &dto.SendCodeRequest{Email: "new@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.StopRegister)
	_, err = f.svc.SendEmailCode(identitytest.Context(), &dto.SendCodeRequest{Email: "not-an-email", Type: uint8(auth.Security)})
	assertCode(t, err, xerr.InvalidParams)
}

// Closing registration must not lock members out of binding or changing an
// address: a signed-in account still gets a register code for an address no
// account holds, to bind it to itself, while an anonymous request is
// refused. The other refusals stay: a disabled method, an address another
// account holds and, for email, the domain allowlist.
func TestSignedInAccountsGetBindCodesWhileRegistrationIsClosed(t *testing.T) {
	f := newCodeFixture(t)
	f.policy.StopRegister = true
	member := user.NewContext(identitytest.Context(), &user.User{Id: 7})

	_, err := f.svc.SendEmailCode(identitytest.Context(), &dto.SendCodeRequest{Email: "new@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.StopRegister)
	_, err = f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.StopRegister)

	if _, err := f.svc.SendEmailCode(member, &dto.SendCodeRequest{Email: "new@example.com", Type: uint8(auth.Register)}); err != nil {
		t.Fatalf("SendEmailCode() for a signed-in account: %v", err)
	}
	if _, err := f.svc.SendSmsCode(member, &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13800138000"}); err != nil {
		t.Fatalf("SendSmsCode() for a signed-in account: %v", err)
	}
	if len(f.queue.tasks) != 2 || !f.Mini.Exists(verification.EmailCodeKey(auth.Register, "new@example.com")) || !f.Mini.Exists(verification.MobileCodeKey(auth.Register, "+8613800138000")) {
		t.Fatalf("tasks = %d, keys = %v; want both codes sent and stored for the binding", len(f.queue.tasks), f.Mini.Keys())
	}

	f.bind(t, "email", "taken@example.com")
	_, err = f.svc.SendEmailCode(member, &dto.SendCodeRequest{Email: "taken@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.UserExist)
	f.cfg.EnableDomainSuffix, f.cfg.DomainSuffixList = true, "example.org"
	_, err = f.svc.SendEmailCode(member, &dto.SendCodeRequest{Email: "other@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.InvalidParams)
	f.policy.EmailEnabled, f.policy.MobileEnabled = false, false
	_, err = f.svc.SendEmailCode(member, &dto.SendCodeRequest{Email: "other@example.org", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.GetAuthenticatorError)
	_, err = f.svc.SendSmsCode(member, &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13900139000"})
	assertCode(t, err, xerr.GetAuthenticatorError)
}
