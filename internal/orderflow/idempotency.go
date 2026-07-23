package orderflow

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/order"
)

// Idempotency carries V2-only creation metadata through the existing domain
// creators. V1 callers never attach it, so their persisted representation and
// behaviour remain unchanged.
type Idempotency struct {
	Key                string
	Hash               string
	GuestCheckoutToken string
}

type idempotencyContextKey struct{}

func WithIdempotency(ctx context.Context, value Idempotency) context.Context {
	return context.WithValue(ctx, idempotencyContextKey{}, value)
}

func ApplyIdempotency(ctx context.Context, data *order.Order) {
	value, ok := ctx.Value(idempotencyContextKey{}).(Idempotency)
	if !ok {
		return
	}
	data.IdempotencyKey = value.Key
	data.IdempotencyHash = value.Hash
}

func GuestCheckoutToken(ctx context.Context) string {
	value, ok := ctx.Value(idempotencyContextKey{}).(Idempotency)
	if !ok {
		return ""
	}
	return value.GuestCheckoutToken
}
