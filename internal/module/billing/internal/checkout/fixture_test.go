package checkout

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// checkoutFixture runs the checkout service against the real repositories
// over SQLite.
type checkoutFixture struct {
	t     *testing.T
	h     *billingtest.Harness
	queue *billingtest.Queue
	svc   *Service
}

func newCheckoutFixture(t *testing.T, adjust ...func(*Deps)) *checkoutFixture {
	t.Helper()
	h := billingtest.New(t)
	queue := &billingtest.Queue{}
	deps := Deps{
		Orders:       h.Store.Order(),
		Coupons:      h.Store.Coupon(),
		Payments:     h.Store.Payment(),
		Plans:        h.Store.Subscribe(),
		UserSubs:     h.Store.UserSubscription(),
		Wallets:      h.Store.Wallet(),
		Tx:           h.Store,
		Inventory:    subscription.NewInventory(h.Store),
		Queue:        queue,
		SingleModel:  func() bool { return false },
		CurrencyUnit: func() string { return "CNY" },
	}
	for _, fn := range adjust {
		fn(&deps)
	}
	return &checkoutFixture{t: t, h: h, queue: queue, svc: NewService(deps)}
}

func withGateways(registry *gateway.Registry) func(*Deps) {
	return func(d *Deps) { d.Gateways = registry }
}

// buyer seeds an account holding gift credit and no balance.
func (f *checkoutFixture) buyer(gift int64) (*user.User, context.Context) {
	f.t.Helper()
	u := f.h.User()
	f.h.Wallet(u.Id, 0, gift)
	return u, billingtest.UserContext(u)
}

// epay seeds an EPay method charging fee.
func (f *checkoutFixture) epay(adjust ...func(*payment.Payment)) *payment.Payment {
	f.t.Helper()
	return f.h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`, adjust...)
}

func percentFee(percent int64) func(*payment.Payment) {
	return func(p *payment.Payment) { p.FeeMode, p.FeePercent = 1, percent }
}

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}
