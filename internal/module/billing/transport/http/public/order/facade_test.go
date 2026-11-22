package order

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
)

// epayConfig configures an EPay method whose checkout is a signed URL built
// locally, so a checkout needs no gateway round trip.
const epayConfig = `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`

// stock is the plan inventory port of the checkout flows (the subscription
// module's inventory in production): it records what the flows reserved and
// returned.
type stock struct {
	reserved []string
	restored []string
}

func (s *stock) Reserve(_ context.Context, orderNo string, _ int64) error {
	s.reserved = append(s.reserved, orderNo)
	return nil
}

func (s *stock) Restore(_ context.Context, orderNo string, _ int64) error {
	s.restored = append(s.restored, orderNo)
	return nil
}

// orderFacade is the billing module assembled like the application assembles
// it, over the real repositories of the billing test harness.
type orderFacade struct {
	h     *billingtest.Harness
	svc   billing.Service
	stock *stock
}

func newOrderFacade(t *testing.T) *orderFacade {
	t.Helper()
	h := billingtest.New(t)
	inventory := &stock{}
	svc := billing.New(billing.Deps{
		Orders: h.Store.Order(), OrderEvents: h.Store.OrderEvent(), Payments: h.Store.Payment(), Coupons: h.Store.Coupon(),
		Withdrawals: h.Store.UserWithdrawal(), Plans: h.Store.Subscribe(), UserSubs: h.Store.UserSubscription(),
		Store: h.Store, Inventory: inventory, Tx: h.Store, Queue: &billingtest.Queue{}, Redis: h.Redis,
		SingleModel: func() bool { return false }, CurrencyUnit: func() string { return "CNY" },
		Logs: h.Store.Log(), UserCache: &billingtest.UserCache{}, Affiliates: h.Store.User(), AuthMethods: h.Store.UserAuth(),
		UserProfiles: h.Store.User(), InvitePolicy: func() (uint8, bool) { return 0, false },
		PortalPlans: h.Store.Subscribe(), GuestAccounts: h.Store.UserAuth(), Sessions: h.Redis, GuestCheckoutCache: h.Redis,
		ExchangeRate: billing.NewCurrencyRateCache(0),
		Portal: billing.PortalConfig{
			SiteHost: func() string { return "panel.example.com" }, SiteName: func() string { return "Panel" },
			CurrencyUnit: func() string { return "CNY" }, JwtSecret: "order-handler-secret", JwtExpire: 3600,
		},
	})
	return &orderFacade{h: h, svc: svc, stock: inventory}
}

// buyer seeds an account with an empty wallet and returns the request
// context the authentication middleware would build for it.
func (f *orderFacade) buyer() (int64, context.Context) {
	u := f.h.User()
	f.h.Wallet(u.Id, 0, 0)
	return u.Id, billingtest.UserContext(u)
}

// guest is the request context of an anonymous caller: it carries no
// account.
var guest = context.Background()

// serve performs one request against the order routes as production
// registers them. account is the request context the optional
// authentication middleware hands on: a signed-in account's (see buyer), or
// guest. A body is sent as JSON.
func (f *orderFacade) serve(account context.Context, method, path, body string, headers ...ut.Header) *ut.ResponseRecorder {
	engine := server.New()
	engine.Use(func(_ context.Context, c *app.RequestContext) { c.Next(account) })
	v2 := engine.Group("/v2/public/orders")
	v2.POST("", V2CreateAndCheckoutHandler(f.svc))
	v2.POST("/:orderNo/checkout", V2CheckoutHandler(f.svc))
	v2.GET("/:orderNo", V2GetOrderHandler(f.svc))
	v2.POST("/:orderNo/event-ticket", V2EventTicketHandler(f.svc))
	v2.POST("/:orderNo/session", V2OrderSessionHandler(f.svc))
	engine.POST("/v1/public/order/close", CloseOrderHandler(f.svc))

	var payload *ut.Body
	if body != "" {
		payload = &ut.Body{Body: strings.NewReader(body), Len: len(body)}
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	return ut.PerformRequest(engine.Engine, method, path, payload, headers...)
}

// envelope is the JSON result every order endpoint answers with.
type envelope struct {
	Code uint32          `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// decode reads the envelope of an HTTP 200 answer; the order endpoints
// report failures in the envelope, not in the status.
func decode(t *testing.T, w *ut.ResponseRecorder) envelope {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", got)
	}
	var result envelope
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return result
}

// assertFailure checks an envelope carrying code and msg without data.
func assertFailure(t *testing.T, w *ut.ResponseRecorder, code uint32, msg string) {
	t.Helper()
	result := decode(t, w)
	if result.Code != code || result.Msg != msg || result.Data != nil {
		t.Fatalf("result = %s, want code %d %q", w.Body.String(), code, msg)
	}
}

// bindingError is the error the framework's JSON binding reports for body
// bound into dest: what a handler that refuses the body must pass on.
func bindingError(t *testing.T, body string, dest any) string {
	t.Helper()
	ctx := app.NewContext(0)
	ctx.Request.Header.SetContentTypeBytes([]byte("application/json"))
	ctx.Request.SetBodyString(body)
	err := ctx.BindJSON(dest)
	if err == nil {
		t.Fatalf("body %q binds", body)
	}
	return err.Error()
}

// success checks a successful envelope and decodes its data into data.
func success(t *testing.T, w *ut.ResponseRecorder, data any) {
	t.Helper()
	result := decode(t, w)
	if result.Code != http.StatusOK || result.Msg != "success" {
		t.Fatalf("result = %s, want success", w.Body.String())
	}
	if data == nil {
		if result.Data != nil {
			t.Fatalf("result carries data %s, want none", result.Data)
		}
		return
	}
	if err := json.Unmarshal(result.Data, data); err != nil {
		t.Fatalf("data %s: %v", result.Data, err)
	}
}
