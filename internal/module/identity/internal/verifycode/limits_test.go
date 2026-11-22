package verifycode

import (
	"context"
	"fmt"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// fromIP is a request context from the client address ip.
func fromIP(ip string) context.Context {
	return requestmeta.With(context.Background(), requestmeta.New(ip, identitytest.UserAgent))
}

// One client address gets a bounded number of codes an hour, whatever the
// addresses and numbers it names, so rotating targets does not get around
// the per-target quotas: a bombing or pumping script is cut off, while
// another address keeps its own quota and the quota returns with the next
// hour. Email and SMS codes share it.
func TestCodesAreCappedPerClientAddress(t *testing.T) {
	f := newCodeFixture(t)
	script := fromIP("198.51.100.7")

	for i := 0; i < CodesPerIPPerHour; i++ {
		var err error
		if i%2 == 0 {
			_, err = f.svc.SendEmailCode(script, &dto.SendCodeRequest{Email: fmt.Sprintf("target-%d@example.com", i), Type: uint8(auth.Register)})
		} else {
			_, err = f.svc.SendSmsCode(script, &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: fmt.Sprintf("1380013%04d", i)})
		}
		if err != nil {
			t.Fatalf("code %d: %v", i, err)
		}
	}
	_, err := f.svc.SendEmailCode(script, &dto.SendCodeRequest{Email: "one-more@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = f.svc.SendSmsCode(script, &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13900139000"})
	assertCode(t, err, xerr.TooManyRequests)
	if len(f.queue.tasks) != CodesPerIPPerHour {
		t.Fatalf("deliveries = %d, want exactly the quota", len(f.queue.tasks))
	}

	if _, err := f.svc.SendEmailCode(fromIP("203.0.113.9"), &dto.SendCodeRequest{Email: "one-more@example.com", Type: uint8(auth.Register)}); err != nil {
		t.Fatalf("another address's code: %v", err)
	}
	f.Mini.FastForward(time.Hour + time.Second)
	if _, err := f.svc.SendEmailCode(script, &dto.SendCodeRequest{Email: "next-hour@example.com", Type: uint8(auth.Register)}); err != nil {
		t.Fatalf("code in the next hour: %v", err)
	}
}

// While registration verification is on, an anonymous request for a
// registration code passes the Turnstile challenge: it starts a registration,
// and a script must not get the deliveries a person gets. A signed-in
// account binding an address, and every security code, skip it.
func TestAnonymousRegistrationCodesNeedTheChallenge(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	queue := &taskRecorder{}
	policy := registerpolicy.Snapshot{EmailEnabled: true, MobileEnabled: true, RegisterVerify: true, TurnstileSecret: "site-secret"}
	var asked int
	svc := NewService(Deps{
		Store: env.Store, Redis: env.Redis, Queue: queue,
		Policy: registerpolicy.New(registerpolicy.Deps{
			Auths:  env.Store.Auth(),
			Config: func() registerpolicy.Snapshot { return policy },
			VerifyTurnstile: func(_ context.Context, _, token, _ string) (bool, error) {
				asked++
				return token == "human", nil
			},
		}),
		Config: func() Snapshot { return Snapshot{VerifyCodeExpire: 300} },
	})
	owner := &user.User{}
	if err := env.DB.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.DB.Create(&user.AuthMethods{UserId: owner.Id, AuthType: "email", AuthIdentifier: "owner@example.com", Verified: true}).Error; err != nil {
		t.Fatal(err)
	}
	anonymous := identitytest.Context()

	_, err := svc.SendEmailCode(anonymous, &dto.SendCodeRequest{Email: "new@example.com", Type: uint8(auth.Register)})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = svc.SendEmailCode(anonymous, &dto.SendCodeRequest{Email: "new@example.com", Type: uint8(auth.Register), CfToken: "robot"})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = svc.SendSmsCode(anonymous, &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.TooManyRequests)
	if len(queue.tasks) != 0 {
		t.Fatalf("deliveries = %d, want none without the challenge", len(queue.tasks))
	}

	if _, err := svc.SendEmailCode(anonymous, &dto.SendCodeRequest{Email: "new@example.com", Type: uint8(auth.Register), CfToken: "human"}); err != nil {
		t.Fatalf("a human's registration code: %v", err)
	}
	if _, err := svc.SendSmsCode(anonymous, &dto.SendSmsCodeRequest{Type: uint8(auth.Register), TelephoneAreaCode: "86", Telephone: "13800138000", CfToken: "human"}); err != nil {
		t.Fatalf("a human's registration code by SMS: %v", err)
	}
	// A security code to a bound address, and a signed-in account's binding
	// code, are not challenged.
	checked := asked
	if _, err := svc.SendEmailCode(anonymous, &dto.SendCodeRequest{Email: "owner@example.com", Type: uint8(auth.Security)}); err != nil {
		t.Fatalf("a security code: %v", err)
	}
	if _, err := svc.SendEmailCode(user.NewContext(anonymous, owner), &dto.SendCodeRequest{Email: "second@example.com", Type: uint8(auth.Register)}); err != nil {
		t.Fatalf("a member's binding code: %v", err)
	}
	if asked != checked {
		t.Fatalf("the challenge was checked %d more times for codes that need none", asked-checked)
	}
	if len(queue.tasks) != 4 {
		t.Fatalf("deliveries = %d, want the four allowed codes", len(queue.tasks))
	}
}
