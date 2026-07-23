package user

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	usermodel "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/constant"
)

type fakeBindOAuthMethodPolicy struct {
	methods []string
}

func (p *fakeBindOAuthMethodPolicy) EnsureMethodEnabled(_ context.Context, method string) error {
	p.methods = append(p.methods, method)
	return nil
}

func TestBindOAuthCallbackUsesInjectedMethodPolicy(t *testing.T) {
	policy := &fakeBindOAuthMethodPolicy{}
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 7})
	logic := NewBindOAuthCallbackLogic(ctx, BindOAuthCallbackDependencies{Policy: policy})

	err := logic.BindOAuthCallback(&dto.BindOAuthCallbackRequest{
		Method:   "google",
		Callback: "not-an-object",
	})
	if err == nil {
		t.Fatal("expected invalid callback error")
	}
	if len(policy.methods) != 1 || policy.methods[0] != "google" {
		t.Fatalf("policy methods = %#v, want [google]", policy.methods)
	}
}
