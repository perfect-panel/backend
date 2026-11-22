package portal

import (
	"context"
	"net/url"
	"sync"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// rateCache is the refreshed CNY rate of the site currency.
type rateCache struct {
	mu   sync.Mutex
	rate float64
}

func (c *rateCache) Get() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate
}

func (c *rateCache) Set(rate float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rate = rate
}

// portalFixture runs the storefront flows against the real repositories
// over SQLite.
type portalFixture struct {
	t        *testing.T
	h        *billingtest.Harness
	queue    *billingtest.Queue
	rates    *rateCache
	currency string
	siteHost string
	svc      *Service
}

func newPortalFixture(t *testing.T, adjust ...func(*Deps)) *portalFixture {
	t.Helper()
	h := billingtest.New(t)
	f := &portalFixture{t: t, h: h, queue: &billingtest.Queue{}, rates: &rateCache{}, currency: "CNY", siteHost: "www.example.test"}
	deps := Deps{
		Orders:             h.Store.Order(),
		OrderEvents:        h.Store.OrderEvent(),
		Coupons:            h.Store.Coupon(),
		Payments:           h.Store.Payment(),
		UserAuths:          h.Store.UserAuth(),
		Plans:              h.Store.Subscribe(),
		Tx:                 h.Store,
		UserCache:          &billingtest.UserCache{},
		Inventory:          subscription.NewInventory(h.Store),
		Sessions:           h.Redis,
		Queue:              f.queue,
		GuestCheckoutCache: h.Redis,
		ExchangeRate:       f.rates,
		Config: Config{
			SiteName:     func() string { return "Panel" },
			CurrencyUnit: func() string { return f.currency },
			SiteHost:     func() string { return f.siteHost },
			JwtSecret:    "jwt-secret",
			JwtExpire:    3600,
		},
	}
	for _, fn := range adjust {
		fn(&deps)
	}
	f.svc = NewService(deps)
	return f
}

// buyer seeds an account holding balance and gift credit.
func (f *portalFixture) buyer(balance, gift int64) (*user.User, context.Context) {
	f.t.Helper()
	u := f.h.User()
	f.h.Wallet(u.Id, balance, gift)
	return u, billingtest.UserContext(u)
}

func (f *portalFixture) epay() *payment.Payment {
	f.t.Helper()
	return f.h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`)
}

func (f *portalFixture) balance() *payment.Payment {
	f.t.Helper()
	return f.h.Payment("balance", "")
}

// pendingOrder seeds a pending order paid with method.
func (f *portalFixture) pendingOrder(orderNo string, userID int64, amount int64, method *payment.Payment, adjust ...func(*order.Order)) *order.Order {
	f.t.Helper()
	o := &order.Order{
		OrderNo: orderNo, UserId: userID, Type: order.TypeSubscribe, Status: order.StatusPending,
		Price: amount, Amount: amount, PaymentId: method.Id, Method: method.Platform,
	}
	for _, fn := range adjust {
		fn(o)
	}
	return f.h.Order(o)
}

// guestToken binds a guest checkout capability to the order.
func guestToken(token string) func(*order.Order) {
	return func(o *order.Order) { o.GuestCheckoutTokenHash = order.CheckoutTokenHash(token) }
}

func (f *portalFixture) checkout(ctx context.Context, orderNo, token string) (*dto.CheckoutOrderResponse, error) {
	return f.svc.Checkout(ctx, &dto.CheckoutOrderRequest{OrderNo: orderNo, CheckoutToken: token})
}

// payURLParam reads a query parameter of an EPay checkout redirect.
func payURLParam(t *testing.T, resp *dto.CheckoutOrderResponse, key string) string {
	t.Helper()
	parsed, err := url.Parse(resp.CheckoutUrl)
	if err != nil {
		t.Fatalf("parse checkout URL %q: %v", resp.CheckoutUrl, err)
	}
	return parsed.Query().Get(key)
}

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// raceTransactor runs compete just before each billing transaction,
// standing in for a concurrent request that committed first.
type raceTransactor struct {
	tx      Transactor
	compete func()
}

var _ Transactor = raceTransactor{}

func (r raceTransactor) InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error {
	r.compete()
	return r.tx.InBillingTx(ctx, fn)
}
