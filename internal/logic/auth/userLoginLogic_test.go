package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/authmethod"
)

type fakeEmailLoginPolicy struct {
	methods []string
	err     error
}

func (p *fakeEmailLoginPolicy) EnsureMethodEnabled(_ context.Context, method string) error {
	p.methods = append(p.methods, method)
	return p.err
}

func TestUserLoginUsesInjectedMethodPolicy(t *testing.T) {
	blocked := errors.New("email login disabled")
	policy := &fakeEmailLoginPolicy{err: blocked}
	logic := NewUserLoginLogic(context.Background(), UserLoginDependencies{Policy: policy})

	_, err := logic.UserLogin(&dto.UserLoginRequest{})
	if !errors.Is(err, blocked) {
		t.Fatalf("UserLogin error = %v, want method policy error", err)
	}
	if len(policy.methods) != 1 || policy.methods[0] != authmethod.Email {
		t.Fatalf("method policy calls = %#v, want [email]", policy.methods)
	}
}
