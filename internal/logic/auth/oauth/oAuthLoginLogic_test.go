package oauth

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
)

type fakeOAuthLoginURLPolicy struct {
	methods []string
	err     error
}

func (p *fakeOAuthLoginURLPolicy) EnsureMethodEnabled(_ context.Context, method string) error {
	p.methods = append(p.methods, method)
	return p.err
}

func TestOAuthLoginUsesInjectedMethodPolicy(t *testing.T) {
	blocked := errors.New("oauth login disabled")
	policy := &fakeOAuthLoginURLPolicy{err: blocked}
	logic := NewOAuthLoginLogic(context.Background(), OAuthLoginURLDependencies{Policy: policy})

	_, err := logic.OAuthLogin(&dto.OAthLoginRequest{Method: "google"})
	if !errors.Is(err, blocked) {
		t.Fatalf("OAuthLogin error = %v, want method policy error", err)
	}
	if len(policy.methods) != 1 || policy.methods[0] != "google" {
		t.Fatalf("method policy calls = %#v, want [google]", policy.methods)
	}
}
