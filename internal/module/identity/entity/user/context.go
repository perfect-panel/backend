package user

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
)

// NewContext returns ctx carrying u as the authenticated user of the request.
func NewContext(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, requestctx.CtxKeyUser, u)
}

// FromContext returns the authenticated user the auth middleware stored in
// ctx, or false when the request is anonymous.
func FromContext(ctx context.Context) (*User, bool) {
	if ctx == nil {
		return nil, false
	}
	u, ok := ctx.Value(requestctx.CtxKeyUser).(*User)
	return u, ok && u != nil
}
