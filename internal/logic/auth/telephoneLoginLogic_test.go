package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/authmethod"
)

type fakeTelephoneLoginPolicy struct {
	methods []string
	err     error
}

func (p *fakeTelephoneLoginPolicy) EnsureMethodEnabled(_ context.Context, method string) error {
	p.methods = append(p.methods, method)
	return p.err
}

func TestTelephoneLoginUsesInjectedMethodPolicy(t *testing.T) {
	blocked := errors.New("mobile login disabled")
	policy := &fakeTelephoneLoginPolicy{err: blocked}
	logic := NewTelephoneLoginLogic(context.Background(), TelephoneLoginDependencies{Policy: policy})

	_, err := logic.TelephoneLogin(&dto.TelephoneLoginRequest{}, "192.0.2.1", "test-agent")
	if !errors.Is(err, blocked) {
		t.Fatalf("TelephoneLogin error = %v, want method policy error", err)
	}
	if len(policy.methods) != 1 || policy.methods[0] != authmethod.Mobile {
		t.Fatalf("method policy calls = %#v, want [mobile]", policy.methods)
	}
}
