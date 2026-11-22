package registerpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

func fromIP(ip string) context.Context {
	return requestmeta.With(context.Background(), requestmeta.New(ip, "test-agent"))
}

func TestTakeIPPermit(t *testing.T) {
	server := miniredis.RunT(t)
	policy := New(Deps{
		Redis: redis.NewClient(&redis.Options{Addr: server.Addr()}),
		Config: func() Snapshot {
			return Snapshot{
				EnableIpRegisterLimit:   true,
				IpRegisterLimit:         2,
				IpRegisterLimitDuration: 10,
			}
		},
	})

	for i := 0; i < 2; i++ {
		if err := policy.TakeIPPermit(fromIP("192.0.2.8")); err != nil {
			t.Fatalf("permit %d: %v", i+1, err)
		}
	}
	if err := policy.TakeIPPermit(fromIP("192.0.2.8")); xerr.CodeOf(err) != xerr.TooManyRequests {
		t.Fatalf("third registration: error = %v, want TooManyRequests", err)
	}
	if err := policy.TakeIPPermit(fromIP("192.0.2.9")); err != nil {
		t.Fatalf("different IP should have its own quota: %v", err)
	}
	// Without a client address there is no quota to charge.
	if err := policy.TakeIPPermit(context.Background()); xerr.CodeOf(err) != xerr.InvalidParams {
		t.Fatalf("request without client address: error = %v, want InvalidParams", err)
	}
}

func TestEnsureRegistrationOpenForEmail(t *testing.T) {
	stopped := false
	policy := New(Deps{Config: func() Snapshot {
		return Snapshot{EmailEnabled: true, StopRegister: stopped}
	}})
	if err := policy.EnsureRegistrationOpen(context.Background(), MethodEmail); err != nil {
		t.Fatalf("enabled registration rejected: %v", err)
	}
	stopped = true
	if err := policy.EnsureRegistrationOpen(context.Background(), MethodEmail); xerr.CodeOf(err) != xerr.StopRegister {
		t.Fatalf("stopped registration: error = %v, want StopRegister", err)
	}
}

func TestEnsureMethodEnabledFollowsTheSwitches(t *testing.T) {
	policy := New(Deps{Config: func() Snapshot { return Snapshot{MobileEnabled: true} }})
	if err := policy.EnsureMethodEnabled(context.Background(), MethodMobile); err != nil {
		t.Fatalf("enabled mobile sign-in rejected: %v", err)
	}
	for _, method := range []string{MethodEmail, MethodDevice} {
		if err := policy.EnsureMethodEnabled(context.Background(), method); xerr.CodeOf(err) != xerr.GetAuthenticatorError {
			t.Fatalf("disabled %s sign-in: error = %v, want GetAuthenticatorError", method, err)
		}
	}
}

// Each purpose follows its own switch, and the token is checked against the
// client address of the request.
func TestVerifyHumanFollowsThePurposeSwitch(t *testing.T) {
	var asked []string
	snapshot := Snapshot{LoginVerify: true, TurnstileSecret: "site-secret"}
	policy := New(Deps{
		Config: func() Snapshot { return snapshot },
		VerifyTurnstile: func(_ context.Context, secret, token, ip string) (bool, error) {
			asked = append(asked, secret+"|"+token+"|"+ip)
			return token == "human", nil
		},
	})
	ctx := fromIP("203.0.113.7")

	for _, purpose := range []Purpose{Register, Reset} {
		if err := policy.VerifyHuman(ctx, purpose, ""); err != nil {
			t.Fatalf("purpose %d is off but was checked: %v", purpose, err)
		}
	}
	if err := policy.VerifyHuman(ctx, Login, "human"); err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}
	if err := policy.VerifyHuman(ctx, Login, "robot"); xerr.CodeOf(err) != xerr.TooManyRequests {
		t.Fatalf("an invalid token: error = %v, want TooManyRequests", err)
	}
	if err := policy.VerifyHuman(ctx, Login, " "); xerr.CodeOf(err) != xerr.TooManyRequests {
		t.Fatalf("a missing token: error = %v, want TooManyRequests", err)
	}
	if len(asked) != 2 || asked[0] != "site-secret|human|203.0.113.7" {
		t.Fatalf("verifier calls = %v", asked)
	}

	snapshot = Snapshot{ResetPasswordVerify: true, TurnstileSecret: "site-secret"}
	policy.deps.VerifyTurnstile = func(context.Context, string, string, string) (bool, error) {
		return false, errors.New("cloudflare unavailable")
	}
	if err := policy.VerifyHuman(ctx, Reset, "human"); xerr.CodeOf(err) != xerr.TooManyRequests {
		t.Fatalf("an unavailable verifier: error = %v, want TooManyRequests", err)
	}
}
