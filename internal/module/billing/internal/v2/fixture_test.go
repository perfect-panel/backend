package v2

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
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

// v2Fixture wires the orchestration over the real checkout and storefront
// services and repositories.
type v2Fixture struct {
	t        *testing.T
	h        *billingtest.Harness
	queue    *billingtest.Queue
	rates    *rateCache
	currency string
	checkout *checkout.Service
	portal   *portal.Service
	svc      *Service
}

type v2Options struct {
	gateways *gateway.Registry
	orders   func(portal.Orders) portal.Orders
	stream   func(*StreamDeps)
	replays  GuestReplayLimits
}

func newV2Fixture(t *testing.T, opts v2Options) *v2Fixture {
	t.Helper()
	h := billingtest.New(t)
	f := &v2Fixture{t: t, h: h, queue: &billingtest.Queue{}, rates: &rateCache{}, currency: "CNY"}
	currency := func() string { return f.currency }
	f.checkout = checkout.NewService(checkout.Deps{
		Orders: h.Store.Order(), Coupons: h.Store.Coupon(), Payments: h.Store.Payment(),
		Plans: h.Store.Subscribe(), UserSubs: h.Store.UserSubscription(), Wallets: h.Store.Wallet(),
		Tx: h.Store, Inventory: subscription.NewInventory(h.Store), Queue: f.queue, Gateways: opts.gateways,
		SingleModel: func() bool { return false }, CurrencyUnit: currency,
	})
	var portalOrders portal.Orders = h.Store.Order()
	if opts.orders != nil {
		portalOrders = opts.orders(portalOrders)
	}
	f.portal = portal.NewService(portal.Deps{
		Orders: portalOrders, Coupons: h.Store.Coupon(), Payments: h.Store.Payment(), UserAuths: h.Store.UserAuth(),
		Plans: h.Store.Subscribe(), Tx: h.Store, UserCache: &billingtest.UserCache{}, Inventory: subscription.NewInventory(h.Store),
		Sessions: h.Redis, Queue: f.queue, GuestCheckoutCache: h.Redis, ExchangeRate: f.rates, Gateways: opts.gateways,
		Config: portal.Config{
			SiteHost: func() string { return "panel.example.test" }, SiteName: func() string { return "Panel" }, CurrencyUnit: currency,
			JwtSecret: "v2-secret", JwtExpire: 3600,
		},
	})
	stream := StreamDeps{
		Events:  h.Store.OrderEvent(),
		Broker:  RedisBroker{Client: h.Redis},
		Limiter: RedisLimiter{Client: h.Redis},
		// A stream catches up quickly so tests need no publisher.
		Timing: StreamTiming{Heartbeat: time.Hour, CatchUp: 10 * time.Millisecond, Resubscribe: time.Hour},
	}
	if opts.stream != nil {
		opts.stream(&stream)
	}
	f.svc = NewService(Deps{
		Orders: h.Store.Order(), Checkout: f.checkout, Portal: f.portal,
		JwtSecret: "v2-secret", CurrencyUnit: currency, GuestReplays: opts.replays, Stream: stream,
	})
	return f
}

// buyer seeds an account holding balance and gift credit.
func (f *v2Fixture) buyer(balance, gift int64) (*user.User, context.Context) {
	f.t.Helper()
	u := f.h.User()
	f.h.Wallet(u.Id, balance, gift)
	return u, billingtest.UserContext(u)
}

func (f *v2Fixture) epay() *payment.Payment {
	f.t.Helper()
	return f.h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`)
}

func (f *v2Fixture) plan(inventory int64) *subscribe.Subscribe {
	f.t.Helper()
	return f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = inventory })
}

func purchase(plan *subscribe.Subscribe, method *payment.Payment) *dto.V2CreateOrderRequest {
	return &dto.V2CreateOrderRequest{Type: v2OrderTypePurchase, PaymentID: method.Id, SubscribeID: plan.Id, Quantity: 1}
}

func guestPurchase(plan *subscribe.Subscribe, method *payment.Payment) *dto.V2CreateOrderRequest {
	req := purchase(plan, method)
	req.Guest = &dto.V2GuestOrderRequest{AuthType: "email", Identifier: "guest@example.com", Password: "guest-password"}
	return req
}

// sinkEvent is one event a stream delivered.
type sinkEvent struct {
	ID, Name string
	Data     json.RawMessage
}

// collectingSink records a stream's events and signals each arrival.
type collectingSink struct {
	mu      sync.Mutex
	events  []sinkEvent
	arrived chan struct{}
}

func newCollectingSink() *collectingSink {
	return &collectingSink{arrived: make(chan struct{}, 256)}
}

func (s *collectingSink) Event(id, name string, data []byte) error {
	s.mu.Lock()
	s.events = append(s.events, sinkEvent{ID: id, Name: name, Data: append(json.RawMessage(nil), data...)})
	s.mu.Unlock()
	s.arrived <- struct{}{}
	return nil
}

func (s *collectingSink) KeepAlive() error { return nil }

func (s *collectingSink) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, len(s.events))
	for i, event := range s.events {
		names[i] = event.Name
	}
	return names
}

func (s *collectingSink) snapshot() []sinkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkEvent(nil), s.events...)
}

// waitFor waits until the sink holds count events.
func (s *collectingSink) waitFor(t *testing.T, count int) []sinkEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if events := s.snapshot(); len(events) >= count {
			return events
		}
		select {
		case <-s.arrived:
		case <-deadline:
			t.Fatalf("stream delivered %v, want %d events", s.names(), count)
		}
	}
}

// runningStream is a stream served in the background.
type runningStream struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// openStream serves a stream until it ends or the test does.
func (f *v2Fixture) openStream(ctx context.Context, req StreamRequest, sink EventSink) *runningStream {
	ctx, cancel := context.WithCancel(ctx)
	r := &runningStream{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = f.svc.StreamEvents(ctx, req, sink)
	}()
	f.t.Cleanup(func() { _ = r.stop() })
	return r
}

// stop ends the stream and returns its result.
func (r *runningStream) stop() error {
	r.cancel()
	<-r.done
	return r.err
}

// wait returns the result of a stream that ends on its own.
func (r *runningStream) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
		return nil
	}
}
