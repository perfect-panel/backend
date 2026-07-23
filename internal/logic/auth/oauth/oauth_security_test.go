package oauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/redis/go-redis/v9"
)

type fakeOAuthRegistrationPolicy struct {
	methodCalls []string
}

func (p *fakeOAuthRegistrationPolicy) EnsureMethodEnabled(_ context.Context, method string) error {
	p.methodCalls = append(p.methodCalls, method)
	return nil
}

func (p *fakeOAuthRegistrationPolicy) EnsureRegistrationOpen(context.Context, string) error {
	return nil
}

func (p *fakeOAuthRegistrationPolicy) VerifyHuman(context.Context, string, string) error {
	return nil
}

func (p *fakeOAuthRegistrationPolicy) TakeIPPermit(context.Context, string) error {
	return nil
}

func TestOAuthClaimBool(t *testing.T) {
	tests := []struct {
		value interface{}
		want  bool
	}{
		{value: true, want: true},
		{value: "true", want: true},
		{value: " TRUE ", want: true},
		{value: false, want: false},
		{value: "false", want: false},
		{value: nil, want: false},
	}
	for _, test := range tests {
		if got := oauthClaimBool(test.value); got != test.want {
			t.Fatalf("oauthClaimBool(%v) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestValidateStateCodeConsumesState(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	if err := client.Set(ctx, "google:state", "https://example.com/callback", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	logic := NewOAuthLoginGetTokenLogic(ctx, OAuthLoginDependencies{Redis: client})

	redirect, err := logic.validateStateCode("google", "state", "request-id")
	if err != nil {
		t.Fatalf("first state validation failed: %v", err)
	}
	if redirect != "https://example.com/callback" {
		t.Fatalf("redirect = %q", redirect)
	}
	if _, err := logic.validateStateCode("google", "state", "request-id"); err == nil {
		t.Fatal("expected consumed OAuth state to be rejected")
	}
	if _, err := logic.validateStateCode("google", "", "request-id"); err == nil || errors.Is(err, redis.Nil) {
		t.Fatal("expected empty OAuth state to be rejected before Redis lookup")
	}
}

func TestOAuthLoginUsesInjectedRegistrationPolicy(t *testing.T) {
	policy := &fakeOAuthRegistrationPolicy{}
	logic := NewOAuthLoginGetTokenLogic(context.Background(), OAuthLoginDependencies{Policy: policy})

	_, err := logic.OAuthLoginGetToken(&dto.OAuthLoginGetTokenRequest{
		Method:   OAuthGoogle,
		Callback: "not-an-object",
	}, "192.0.2.1", "test-agent")
	if err == nil {
		t.Fatal("expected invalid callback error")
	}
	if len(policy.methodCalls) != 1 || policy.methodCalls[0] != OAuthGoogle {
		t.Fatalf("policy method calls = %#v, want [google]", policy.methodCalls)
	}
}
