package v2

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// countingLimiter grants quota permits per key and counts the calls.
type countingLimiter struct {
	quota int
	taken map[string]int
	err   error
}

func newCountingLimiter(quota int) *countingLimiter {
	return &countingLimiter{quota: quota, taken: map[string]int{}}
}

func (l *countingLimiter) Take(_ context.Context, key string) (int, error) {
	if l.err != nil {
		return ratelimit.Unknown, l.err
	}
	l.taken[key]++
	switch {
	case l.taken[key] < l.quota:
		return ratelimit.Allowed, nil
	case l.taken[key] == l.quota:
		return ratelimit.HitQuota, nil
	default:
		return ratelimit.OverQuota, nil
	}
}

// fromIP is an anonymous request from the client address.
func fromIP(ip string) context.Context {
	return requestmeta.With(context.Background(), requestmeta.New(ip, "browser"))
}

// A guest replay of a create request proves the guest password against the
// order, so whoever holds the idempotency key can test passwords through
// the 409-or-200 answer. Replays are limited per key: past the quota every
// replay, right password or wrong, is refused as too many requests; the
// creation itself and other keys are unaffected.
func TestGuestReplaysAreLimitedPerKey(t *testing.T) {
	perKey := newCountingLimiter(3)
	f := newV2Fixture(t, v2Options{replays: GuestReplayLimits{PerKey: perKey}})
	plan, method := f.plan(5), f.epay()
	ctx := fromIP("203.0.113.9")

	created, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000101")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	if len(perKey.taken) != 0 {
		t.Fatal("the creation itself took a replay permit")
	}
	wrong := guestPurchase(plan, method)
	wrong.Guest.Password = "wrong-password"
	for i := range 3 {
		if _, err := f.svc.CreateAndCheckout(ctx, wrong, "key-0000000101"); !errors.Is(err, ErrIdempotencyKeyReused) {
			t.Fatalf("replay %d with a wrong password = %v, want ErrIdempotencyKeyReused", i+1, err)
		}
	}
	_, err = f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000101")
	assertCode(t, err, xerr.TooManyRequests)
	_, err = f.svc.CreateAndCheckout(ctx, wrong, "key-0000000101")
	assertCode(t, err, xerr.TooManyRequests)
	if perKey.taken["key-0000000101"] != 5 {
		t.Fatalf("permits taken = %v, want one per replay", perKey.taken)
	}
	// Another key is another quota, and the first order is intact.
	if _, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000102"); err != nil {
		t.Fatalf("another key: %v", err)
	}
	if o := f.h.ReloadOrder(created.Order.OrderNo); o.Status != 1 {
		t.Fatalf("order = %+v, want it still pending", o)
	}
}

// Replays are also limited per client address, across keys, so a holder of
// many keys cannot test many orders from one place; a limiter that cannot
// answer refuses rather than opening the oracle.
func TestGuestReplaysAreLimitedPerClientIP(t *testing.T) {
	perIP := newCountingLimiter(2)
	f := newV2Fixture(t, v2Options{replays: GuestReplayLimits{PerIP: perIP}})
	plan, method := f.plan(5), f.epay()
	ctx := fromIP("203.0.113.10")
	for _, key := range []string{"key-0000000201", "key-0000000202"} {
		if _, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), key); err != nil {
			t.Fatalf("CreateAndCheckout %s: %v", key, err)
		}
	}
	for _, key := range []string{"key-0000000201", "key-0000000202"} {
		if _, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), key); err != nil {
			t.Fatalf("replay %s: %v", key, err)
		}
	}
	_, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000201")
	assertCode(t, err, xerr.TooManyRequests)
	if perIP.taken["203.0.113.10"] != 3 {
		t.Fatalf("permits taken = %v, want one per replay from the address", perIP.taken)
	}
	// Another address has its own quota.
	if _, err := f.svc.CreateAndCheckout(fromIP("203.0.113.11"), guestPurchase(plan, method), "key-0000000201"); err != nil {
		t.Fatalf("replay from another address: %v", err)
	}

	perIP.err = errors.New("limit store down")
	_, err = f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000202")
	assertCode(t, err, xerr.TooManyRequests)
}

// A signed-in user's replay is bound to the account, not a password, and is
// not limited.
func TestUserReplaysAreNotLimited(t *testing.T) {
	perKey, perIP := newCountingLimiter(1), newCountingLimiter(1)
	f := newV2Fixture(t, v2Options{replays: GuestReplayLimits{PerKey: perKey, PerIP: perIP}})
	_, ctx := f.buyer(0, 0)
	ctx = requestmeta.With(ctx, requestmeta.New("203.0.113.12", "browser"))
	plan, method := f.plan(5), f.epay()
	for range 4 {
		if _, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000301"); err != nil {
			t.Fatalf("CreateAndCheckout: %v", err)
		}
	}
	if len(perKey.taken) != 0 || len(perIP.taken) != 0 {
		t.Fatalf("permits taken = %v %v, want none for a signed-in user", perKey.taken, perIP.taken)
	}
}

// The production limiter is the auth rate limiter over Redis.
func TestGuestReplayLimitWorksOverRedis(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	f.svc.deps.GuestReplays = GuestReplayLimits{PerKey: ratelimit.NewPeriodLimit(900, 2, f.h.Redis, "test:v2:guest-replay:key:")}
	plan, method := f.plan(5), f.epay()
	ctx := fromIP("203.0.113.13")
	if _, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000401"); err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	for range 2 {
		if _, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000401"); err != nil {
			t.Fatalf("replay within the quota: %v", err)
		}
	}
	_, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000401")
	assertCode(t, err, xerr.TooManyRequests)
}
