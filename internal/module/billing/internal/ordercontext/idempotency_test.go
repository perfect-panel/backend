package ordercontext

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
)

func TestIdempotencyTravelsWithTheContext(t *testing.T) {
	ctx := WithIdempotency(context.Background(), Idempotency{Key: "key-1", Hash: "hash-1", GuestCheckoutToken: "guest-1"})
	var o order.Order
	ApplyIdempotency(ctx, &o)
	if o.IdempotencyKey != "key-1" || o.IdempotencyHash != "hash-1" {
		t.Fatalf("order = %+v, want the key and hash applied", o)
	}
	if got := GuestCheckoutToken(ctx); got != "guest-1" {
		t.Fatalf("GuestCheckoutToken = %q", got)
	}
}

// V1 callers never attach metadata; their orders keep empty fields.
func TestWithoutIdempotencyNothingIsApplied(t *testing.T) {
	o := order.Order{IdempotencyKey: "unchanged"}
	ApplyIdempotency(context.Background(), &o)
	if o.IdempotencyKey != "unchanged" || o.IdempotencyHash != "" || GuestCheckoutToken(context.Background()) != "" {
		t.Fatalf("order = %+v", o)
	}
}
