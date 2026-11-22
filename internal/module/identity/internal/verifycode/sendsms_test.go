package verifycode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	dto "github.com/perfect-panel/server/internal/module/identity/contract"
	"github.com/perfect-panel/server/internal/module/identity/entity/auth"
	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/server/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/server/pkg/logger/logtest"
	"github.com/perfect-panel/server/pkg/xerr"
)

// taskRecorder keeps the tasks the flows enqueue.
type taskRecorder struct{ tasks []*asynq.Task }

func (q *taskRecorder) EnqueueContext(_ context.Context, task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "task"}, nil
}

type codeFixture struct {
	*identitytest.Env
	svc    *Service
	queue  *taskRecorder
	policy *registerpolicy.Snapshot
	cfg    *Snapshot
}

func newCodeFixture(t *testing.T) *codeFixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	queue := &taskRecorder{}
	policy := &registerpolicy.Snapshot{EmailEnabled: true, MobileEnabled: true}
	cfg := &Snapshot{VerifyCodeExpire: 300}
	svc := NewService(Deps{
		Store:  env.Store,
		Redis:  env.Redis,
		Queue:  queue,
		Policy: registerpolicy.New(registerpolicy.Deps{Auths: env.Store.Auth(), Config: func() registerpolicy.Snapshot { return *policy }}),
		Config: func() Snapshot { return *cfg },
	})
	return &codeFixture{Env: env, svc: svc, queue: queue, policy: policy, cfg: cfg}
}

func (f *codeFixture) bind(t *testing.T, authType, identifier string) {
	t.Helper()
	u := &user.User{}
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Create(&user.AuthMethods{UserId: u.Id, AuthType: authType, AuthIdentifier: identifier}).Error; err != nil {
		t.Fatal(err)
	}
}

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// The SMS code is stored under the E.164 number, and the pre-check finds it
// however the client writes the number; it used to prefix the raw input with
// "+" and miss codes of numbers written with separators.
func TestSmsCodeIsFoundWhateverTheNumberLooksLike(t *testing.T) {
	f := newCodeFixture(t)
	if _, err := f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{
		Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "138 0013 8000",
	}); err != nil {
		t.Fatalf("SendSmsCode() error = %v", err)
	}
	if len(f.queue.tasks) != 1 || f.queue.tasks[0].Type() != taskqueue.ForthwithSendSms {
		t.Fatalf("tasks = %+v", f.queue.tasks)
	}
	var payload taskqueue.SendSmsPayload
	if err := json.Unmarshal(f.queue.tasks[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ClientIP != identitytest.ClientIP || payload.Content == "" {
		t.Fatalf("payload = %+v", payload)
	}

	for _, account := range []string{"8613800138000", "86 138 0013 8000", "86-138-0013-8000"} {
		resp, err := f.svc.CheckVerificationCode(context.Background(), &dto.CheckVerificationCodeRequest{
			Method: "mobile", Account: account, Code: payload.Content, Type: uint8(auth.Register),
		})
		if err != nil || !resp.Status {
			t.Fatalf("CheckVerificationCode(%q) = %+v, %v; want the code found", account, resp, err)
		}
	}
	resp, err := f.svc.CheckVerificationCode(context.Background(), &dto.CheckVerificationCodeRequest{
		Method: "mobile", Account: "8613800138000", Code: "000000", Type: uint8(auth.Register),
	})
	if err != nil || resp.Status {
		t.Fatalf("a wrong code = %+v, %v", resp, err)
	}
}

// A register code goes to numbers no account has, a security code to
// numbers an account has.
func TestSmsCodePurposeFollowsTheBinding(t *testing.T) {
	f := newCodeFixture(t)
	f.bind(t, "mobile", "+8613800138000")

	_, err := f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.UserExist)
	_, err = f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Security), TelephoneAreaCode: "86", Telephone: "13900139000"})
	assertCode(t, err, xerr.UserNotExist)
	if _, err := f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Security), TelephoneAreaCode: "86", Telephone: "13800138000"}); err != nil {
		t.Fatalf("security code to the bound number: %v", err)
	}
	// One code per interval.
	_, err = f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Security), TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.TooManyRequests)
}

func TestSmsCodeRespectsThePolicy(t *testing.T) {
	f := newCodeFixture(t)
	f.policy.StopRegister = true
	_, err := f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.StopRegister)
	f.policy.MobileEnabled = false
	_, err = f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: uint8(auth.Security), TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.GetAuthenticatorError)
	if len(f.queue.tasks) != 0 {
		t.Fatalf("tasks = %d, want none", len(f.queue.tasks))
	}
}

// With the whitelist on, a code is never sent (and never costs the operator)
// outside the configured area codes.
func TestSendSmsCodeEnforcesAreaCodeWhitelist(t *testing.T) {
	f := newCodeFixture(t)
	f.cfg.MobileWhitelistEnabled = true
	f.cfg.MobileWhitelist = []string{"86", "+852"}

	_, err := f.svc.SendSmsCode(identitytest.Context(), &dto.SendSmsCodeRequest{Type: 1, TelephoneAreaCode: "882", Telephone: "1234567"})
	assertCode(t, err, xerr.TelephoneError)
	if len(f.queue.tasks) != 0 {
		t.Fatalf("tasks = %d, want none", len(f.queue.tasks))
	}
}

func TestAreaCodeAllowed(t *testing.T) {
	whitelist := []string{"86", "+852", " 1 "}
	for areaCode, want := range map[string]bool{"86": true, "+86": true, "852": true, "1": true, "+1": true, "882": false, "": false, "8": false} {
		if got := areaCodeAllowed(areaCode, whitelist); got != want {
			t.Errorf("areaCodeAllowed(%q) = %v, want %v", areaCode, got, want)
		}
	}
}
