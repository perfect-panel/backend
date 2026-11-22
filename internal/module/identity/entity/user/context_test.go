package user

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
)

func TestFromContext(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("anonymous context reported a user")
	}
	var nilCtx context.Context
	if _, ok := FromContext(nilCtx); ok {
		t.Fatal("nil context reported a user")
	}
	if _, ok := FromContext(context.WithValue(context.Background(), requestctx.CtxKeyUser, "not a user")); ok {
		t.Fatal("a foreign value was accepted as the user")
	}
	var missing *User
	if _, ok := FromContext(NewContext(context.Background(), missing)); ok {
		t.Fatal("a nil user was reported as authenticated")
	}
	u := &User{Id: 7}
	if got, ok := FromContext(NewContext(context.Background(), u)); !ok || got != u {
		t.Fatalf("FromContext = %v, %v; want the stored user", got, ok)
	}
}
