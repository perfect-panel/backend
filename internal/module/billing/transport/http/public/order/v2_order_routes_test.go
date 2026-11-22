package order

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const idempotencyKeyRule = "Idempotency-Key must contain 16-128 printable ASCII characters"

func idempotencyKey(value string) ut.Header { return ut.Header{Key: "Idempotency-Key", Value: value} }

func jsonBody(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// v2Orders is the facade with a plan on sale and an EPay method, the
// purchase every V2 test starts from.
type v2Orders struct {
	*orderFacade
	planID, paymentID int64
	keys              int
}

func newV2Orders(t *testing.T) *v2Orders {
	t.Helper()
	f := newOrderFacade(t)
	return &v2Orders{orderFacade: f, planID: f.h.Plan(1000).Id, paymentID: f.h.Payment("EPay", epayConfig).Id}
}

func (f *v2Orders) purchase() dto.V2CreateOrderRequest {
	return dto.V2CreateOrderRequest{Type: "purchase", PaymentID: f.paymentID, SubscribeID: f.planID, Quantity: 1}
}

func (f *v2Orders) nextKey() string {
	f.keys++
	return fmt.Sprintf("v2-order-key-%06d", f.keys)
}

// userOrder creates a pending purchase of account through the facade.
func (f *v2Orders) userOrder(t *testing.T, account context.Context) string {
	t.Helper()
	req := f.purchase()
	resp, err := f.svc.V2CreateAndCheckout(account, &req, f.nextKey())
	if err != nil {
		t.Fatalf("V2CreateAndCheckout: %v", err)
	}
	return resp.Order.OrderNo
}

// guestOrder creates a pending guest purchase through the facade and returns
// its number and checkout capability.
func (f *v2Orders) guestOrder(t *testing.T) (orderNo, capability string) {
	t.Helper()
	key := f.nextKey()
	req := f.purchase()
	req.Guest = &dto.V2GuestOrderRequest{AuthType: "email", Identifier: key + "@example.com", Password: "guest-password"}
	resp, err := f.svc.V2CreateAndCheckout(guest, &req, key)
	if err != nil {
		t.Fatalf("guest V2CreateAndCheckout: %v", err)
	}
	if resp.CheckoutToken == "" {
		t.Fatal("a guest order came without its checkout capability")
	}
	return resp.Order.OrderNo, resp.CheckoutToken
}

// assertEvents checks the stream reference of an order: its events URL with
// a ticket that has not expired.
func assertEvents(t *testing.T, orderNo, eventsURL string, expiresAt int64) {
	t.Helper()
	prefix := "/v2/public/orders/" + orderNo + "/events?ticket="
	if !strings.HasPrefix(eventsURL, prefix) || len(eventsURL) == len(prefix) {
		t.Fatalf("events URL = %q, want %q followed by a ticket", eventsURL, prefix)
	}
	if expiresAt <= time.Now().Unix() {
		t.Fatalf("ticket expires at %d, want a future time", expiresAt)
	}
}

// assertCheckout checks the order and payment a checkout answered for a
// pending EPay order: the gateway's payment page for the order, sending the
// buyer back to returnURL.
func assertCheckout(t *testing.T, resp dto.V2OrderResponse, stored *order.Order, returnURL string) {
	t.Helper()
	want := dto.V2OrderSnapshot{
		OrderNo: stored.OrderNo, Status: "pending_payment", PaymentStatus: "pending", FulfillmentStatus: "not_started",
		StateVersion: stored.StateVersion, Amount: 1000, Currency: "CNY",
		ExpiresAt: stored.CreatedAt.Add(order.PaymentWindow).Unix(),
	}
	if resp.Order != want {
		t.Fatalf("order = %+v, want %+v", resp.Order, want)
	}
	if resp.Payment == nil || resp.Payment.Type != "url" || resp.Payment.PaymentStatus != "pending" {
		t.Fatalf("payment = %+v, want a pending URL checkout", resp.Payment)
	}
	page, err := url.Parse(resp.Payment.CheckoutURL)
	if err != nil || page.Host != "pay.example" {
		t.Fatalf("checkout URL = %q, want the EPay payment page", resp.Payment.CheckoutURL)
	}
	if query := page.Query(); query.Get("out_trade_no") != stored.OrderNo || query.Get("return_url") != returnURL {
		t.Fatalf("checkout URL = %q, want order %s returning to %q", resp.Payment.CheckoutURL, stored.OrderNo, returnURL)
	}
	assertEvents(t, stored.OrderNo, resp.Events.URL, resp.Events.TicketExpiresAt)
}

// v2OrderEndpoint is an endpoint on an existing order.
type v2OrderEndpoint struct {
	name, method, path string
}

// The endpoints on an existing order: the checkout capability travels in the
// query of the snapshot read and in the JSON body of the others.
var (
	checkoutEndpoint = v2OrderEndpoint{"checkout", http.MethodPost, "/v2/public/orders/%s/checkout"}
	orderEndpoint    = v2OrderEndpoint{"order", http.MethodGet, "/v2/public/orders/%s"}
	ticketEndpoint   = v2OrderEndpoint{"event ticket", http.MethodPost, "/v2/public/orders/%s/event-ticket"}
	sessionEndpoint  = v2OrderEndpoint{"session", http.MethodPost, "/v2/public/orders/%s/session"}
	v2OrderEndpoints = []v2OrderEndpoint{checkoutEndpoint, orderEndpoint, ticketEndpoint, sessionEndpoint}
)

// call requests the endpoint for orderNo, presenting capability the way the
// endpoint takes it.
func (e v2OrderEndpoint) call(t *testing.T, f *v2Orders, account context.Context, orderNo, capability string) *ut.ResponseRecorder {
	t.Helper()
	path := fmt.Sprintf(e.path, url.PathEscape(orderNo))
	if e.method == http.MethodGet {
		return f.serve(account, e.method, path+"?checkout_token="+url.QueryEscape(capability), "")
	}
	return f.serve(account, e.method, path, jsonBody(t, map[string]string{"checkout_token": capability}))
}

func TestValidIdempotencyKeyAcceptsOnly16To128PrintableASCIICharacters(t *testing.T) {
	for name, tt := range map[string]struct {
		key   string
		valid bool
	}{
		"empty":                 {key: ""},
		"15 characters":         {key: strings.Repeat("k", 15)},
		"16 characters":         {key: strings.Repeat("k", 16), valid: true},
		"128 characters":        {key: strings.Repeat("k", 128), valid: true},
		"129 characters":        {key: strings.Repeat("k", 129)},
		"printable range edges": {key: "!~!~!~!~!~!~!~!~", valid: true},
		"inner space":           {key: "order key 000001"},
		"control character":     {key: "order-key\t000001"},
		"delete character":      {key: "order-key-00001\x7f"},
		"non-ASCII letter":      {key: "order-key-0000é"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := validIdempotencyKey(tt.key); got != tt.valid {
				t.Fatalf("validIdempotencyKey(%q) = %v, want %v", tt.key, got, tt.valid)
			}
		})
	}
}

// The idempotency key is what makes a retried submission safe, so a request
// without a usable one is refused before anything is created.
func TestV2CreateAndCheckoutHandlerRefusesAnUnusableIdempotencyKey(t *testing.T) {
	f := newV2Orders(t)
	buyerID, account := f.buyer()
	body := jsonBody(t, f.purchase())
	for name, headers := range map[string][]ut.Header{
		"no key":           nil,
		"blank key":        {idempotencyKey("                    ")},
		"too short":        {idempotencyKey("order-key-00001")},
		"too long":         {idempotencyKey(strings.Repeat("k", 129))},
		"inner space":      {idempotencyKey("order key 000001")},
		"non-ASCII letter": {idempotencyKey("order-key-0000é")},
	} {
		t.Run(name, func(t *testing.T) {
			assertFailure(t, f.serve(account, http.MethodPost, "/v2/public/orders", body, headers...), xerr.InvalidParams, idempotencyKeyRule)
		})
	}
	if orders := f.h.Orders(buyerID); len(orders) != 0 || len(f.stock.reserved) != 0 {
		t.Fatalf("a refused request created %d orders and reserved %v", len(orders), f.stock.reserved)
	}
}

// A body that does not bind is refused with the binding error before the
// facade sees the request; a well-formed body the facade refuses carries the
// facade's code instead.
func TestV2CreateAndCheckoutHandlerRefusesInvalidRequests(t *testing.T) {
	f := newV2Orders(t)
	buyerID, account := f.buyer()
	for name, body := range map[string]string{
		"truncated JSON":  `{"type":"purchase","payment_id":`,
		"wrong JSON type": `{"type":"purchase","payment_id":"one"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := f.serve(account, http.MethodPost, "/v2/public/orders", body, idempotencyKey(f.nextKey()))
			assertFailure(t, w, xerr.InvalidParams, bindingError(t, body, &dto.V2CreateOrderRequest{}))
		})
	}
	renewal := dto.V2CreateOrderRequest{Type: "renewal", PaymentID: f.paymentID, UserSubscribeID: 1, Quantity: 1}
	unknownType := dto.V2CreateOrderRequest{Type: "gift", PaymentID: f.paymentID}
	noPayment := f.purchase()
	noPayment.PaymentID = 0
	for name, tt := range map[string]struct {
		account context.Context
		req     dto.V2CreateOrderRequest
	}{
		"renewal by a guest":                 {account: guest, req: renewal},
		"guest purchase without credentials": {account: guest, req: f.purchase()},
		"unsupported order type":             {account: account, req: unknownType},
		"missing payment method":             {account: account, req: noPayment},
	} {
		t.Run(name, func(t *testing.T) {
			w := f.serve(tt.account, http.MethodPost, "/v2/public/orders", jsonBody(t, tt.req), idempotencyKey(f.nextKey()))
			assertFailure(t, w, xerr.InvalidParams, "Param Error")
		})
	}
	if orders := f.h.Orders(buyerID); len(orders) != 0 || len(f.stock.reserved) != 0 {
		t.Fatalf("a refused request created %d orders and reserved %v", len(orders), f.stock.reserved)
	}
}

// Design: one key yields one order; the retry of a lost response answers the
// same order, while another request under the key is a conflict (HTTP 409)
// that leaves the original order intact.
func TestV2CreateAndCheckoutHandlerCreatesOneOrderPerIdempotencyKey(t *testing.T) {
	f := newV2Orders(t)
	buyerID, account := f.buyer()
	req := f.purchase()
	req.ReturnURL = "https://panel.example.com/result"
	body := jsonBody(t, req)

	// Surrounding blanks are not part of the key.
	var created dto.V2OrderResponse
	success(t, f.serve(account, http.MethodPost, "/v2/public/orders", body, idempotencyKey("  create-key-000001 ")), &created)
	orders := f.h.Orders(buyerID)
	if len(orders) != 1 || orders[0].IdempotencyKey != "create-key-000001" || orders[0].Status != order.StatusPending {
		t.Fatalf("orders = %+v, want one pending order under the trimmed key", orders)
	}
	assertCheckout(t, created, orders[0], "https://panel.example.com/result")
	if created.CheckoutToken != "" {
		t.Fatalf("a signed-in buyer received a guest capability %q", created.CheckoutToken)
	}
	if len(f.stock.reserved) != 1 || f.stock.reserved[0] != orders[0].OrderNo {
		t.Fatalf("reserved = %v, want the order's unit", f.stock.reserved)
	}

	var retried dto.V2OrderResponse
	success(t, f.serve(account, http.MethodPost, "/v2/public/orders", body, idempotencyKey("create-key-000001")), &retried)
	if retried.Order.OrderNo != created.Order.OrderNo || retried.Payment == nil || retried.Payment.CheckoutURL != created.Payment.CheckoutURL {
		t.Fatalf("retry = %+v, want the first order and checkout", retried)
	}

	changed := f.purchase()
	changed.Quantity = 2
	w := f.serve(account, http.MethodPost, "/v2/public/orders", jsonBody(t, changed), idempotencyKey("create-key-000001"))
	if w.Code != http.StatusConflict || w.Body.String() != `{"code":400,"msg":"IDEMPOTENCY_KEY_REUSED"}` {
		t.Fatalf("reused key = %d %s, want 409 IDEMPOTENCY_KEY_REUSED", w.Code, w.Body.String())
	}
	if orders := f.h.Orders(buyerID); len(orders) != 1 || orders[0].Quantity != 1 || len(f.stock.reserved) != 1 {
		t.Fatalf("the retry and the conflict changed the orders: %+v, reserved %v", orders, f.stock.reserved)
	}
}

// A guest has no session: the order answers with the checkout capability
// that authorizes the guest on the other order endpoints.
func TestV2CreateAndCheckoutHandlerGivesAGuestItsCheckoutCapability(t *testing.T) {
	f := newV2Orders(t)
	req := f.purchase()
	req.Guest = &dto.V2GuestOrderRequest{AuthType: "email", Identifier: "Guest@Example.com", Password: "guest-password"}

	var resp dto.V2OrderResponse
	success(t, f.serve(guest, http.MethodPost, "/v2/public/orders", jsonBody(t, req), idempotencyKey("guest-key-0000001")), &resp)
	stored := f.h.ReloadOrder(resp.Order.OrderNo)
	assertCheckout(t, resp, stored, "")
	if resp.CheckoutToken == "" || stored.GuestCheckoutTokenHash != order.CheckoutTokenHash(resp.CheckoutToken) {
		t.Fatalf("checkout capability %q does not open order %s", resp.CheckoutToken, stored.OrderNo)
	}
	if stored.UserId != 0 || stored.GuestIdentifier != "guest@example.com" || stored.IdempotencyKey != "guest-key-0000001" {
		t.Fatalf("guest order = %+v, want the normalized guest identity under the key", stored)
	}
}

// Every order endpoint answers an unknown order, and an order the caller may
// not see, in the envelope.
func TestV2OrderEndpointsRefuseUnknownAndForeignOrders(t *testing.T) {
	f := newV2Orders(t)
	_, owner := f.buyer()
	_, stranger := f.buyer()
	userOrder := f.userOrder(t, owner)
	guestOrder, _ := f.guestOrder(t)
	for _, endpoint := range v2OrderEndpoints {
		for name, tt := range map[string]struct {
			account             context.Context
			orderNo, capability string
			code                uint32
			msg                 string
		}{
			"unknown order":                  {account: owner, orderNo: "no-such-order", code: xerr.OrderNotExist, msg: "Order does not exist"},
			"another user's order":           {account: stranger, orderNo: userOrder, code: xerr.InvalidAccess, msg: "Invalid access"},
			"a user's order to a guest":      {account: guest, orderNo: userOrder, code: xerr.InvalidAccess, msg: "Invalid access"},
			"guest order without capability": {account: guest, orderNo: guestOrder, code: xerr.InvalidAccess, msg: "Invalid access"},
			"guest order, wrong capability":  {account: guest, orderNo: guestOrder, capability: "not-the-capability", code: xerr.InvalidAccess, msg: "Invalid access"},
		} {
			t.Run(endpoint.name+"/"+name, func(t *testing.T) {
				assertFailure(t, endpoint.call(t, f, tt.account, tt.orderNo, tt.capability), tt.code, tt.msg)
			})
		}
	}
}

// The endpoints on an order bind their JSON body into their own request
// before the facade sees it, and a body that does not bind is refused with
// the binding error.
func TestV2OrderEndpointsRefuseAMalformedBody(t *testing.T) {
	f := newV2Orders(t)
	_, owner := f.buyer()
	orderNo := f.userOrder(t, owner)
	for endpoint, request := range map[v2OrderEndpoint]any{
		checkoutEndpoint: &dto.V2CheckoutOrderRequest{},
		ticketEndpoint:   &dto.V2EventTicketRequest{},
		sessionEndpoint:  &dto.V2OrderSessionRequest{},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			const body = `{"checkout_token":7}`
			w := f.serve(owner, endpoint.method, fmt.Sprintf(endpoint.path, orderNo), body)
			assertFailure(t, w, xerr.InvalidParams, bindingError(t, body, request))
		})
	}
}

// A pending order's payment can be resumed by its owner, or by the guest
// holding its capability, with a fresh checkout; a closed order cannot.
func TestV2CheckoutHandlerResumesThePaymentOfAPendingOrder(t *testing.T) {
	f := newV2Orders(t)
	_, owner := f.buyer()
	userOrder := f.userOrder(t, owner)
	guestOrder, capability := f.guestOrder(t)

	var resumed dto.V2OrderResponse
	success(t, f.serve(owner, http.MethodPost, "/v2/public/orders/"+userOrder+"/checkout", `{"return_url":"https://panel.example.com/again"}`), &resumed)
	assertCheckout(t, resumed, f.h.ReloadOrder(userOrder), "https://panel.example.com/again")
	if resumed.CheckoutToken != "" {
		t.Fatalf("the owner's checkout answered a guest capability %q", resumed.CheckoutToken)
	}

	var guestCheckout dto.V2OrderResponse
	success(t, checkoutEndpoint.call(t, f, guest, guestOrder, capability), &guestCheckout)
	assertCheckout(t, guestCheckout, f.h.ReloadOrder(guestOrder), "")
	if guestCheckout.CheckoutToken != capability {
		t.Fatalf("guest checkout capability = %q, want it answered back for recovery", guestCheckout.CheckoutToken)
	}

	if err := f.svc.CloseOrder(owner, &dto.CloseOrderRequest{OrderNo: userOrder}); err != nil {
		t.Fatalf("CloseOrder: %v", err)
	}
	assertFailure(t, f.serve(owner, http.MethodPost, "/v2/public/orders/"+userOrder+"/checkout", `{}`), xerr.OrderStatusError, "Order status error")
}

// The snapshot read describes the order and hands out a fresh stream ticket,
// without starting a payment.
func TestV2GetOrderHandlerDescribesTheOrderWithAStreamTicket(t *testing.T) {
	f := newV2Orders(t)
	_, owner := f.buyer()
	userOrder := f.userOrder(t, owner)
	guestOrder, capability := f.guestOrder(t)
	for name, tt := range map[string]struct {
		account             context.Context
		orderNo, capability string
	}{
		"owner":                {account: owner, orderNo: userOrder},
		"guest with its token": {account: guest, orderNo: guestOrder, capability: capability},
	} {
		t.Run(name, func(t *testing.T) {
			var resp dto.V2OrderResponse
			success(t, orderEndpoint.call(t, f, tt.account, tt.orderNo, tt.capability), &resp)
			stored := f.h.ReloadOrder(tt.orderNo)
			if resp.Order.OrderNo != stored.OrderNo || resp.Order.Status != "pending_payment" || resp.Order.StateVersion != stored.StateVersion {
				t.Fatalf("order = %+v, want the snapshot of %s", resp.Order, stored.OrderNo)
			}
			if resp.Payment != nil || resp.CheckoutToken != "" {
				t.Fatalf("snapshot = %+v, want no payment and no capability", resp)
			}
			assertEvents(t, stored.OrderNo, resp.Events.URL, resp.Events.TicketExpiresAt)
		})
	}
}

// A client whose stream ticket expired refreshes it with its session or its
// guest capability.
func TestV2EventTicketHandlerRefreshesTheStreamTicket(t *testing.T) {
	f := newV2Orders(t)
	_, owner := f.buyer()
	userOrder := f.userOrder(t, owner)
	guestOrder, capability := f.guestOrder(t)
	for name, tt := range map[string]struct {
		account             context.Context
		orderNo, capability string
	}{
		"owner":                {account: owner, orderNo: userOrder},
		"guest with its token": {account: guest, orderNo: guestOrder, capability: capability},
	} {
		t.Run(name, func(t *testing.T) {
			var resp dto.V2EventTicketResponse
			success(t, ticketEndpoint.call(t, f, tt.account, tt.orderNo, tt.capability), &resp)
			assertEvents(t, tt.orderNo, resp.URL, resp.TicketExpiresAt)
		})
	}
}

// Design: a guest's capability becomes a normal session only once the paid
// order has created the guest's account; a user's own order has no
// capability to exchange.
func TestV2OrderSessionHandlerExchangesTheGuestCapabilityOnceTheAccountExists(t *testing.T) {
	f := newV2Orders(t)
	_, owner := f.buyer()
	userOrder := f.userOrder(t, owner)
	guestOrder, capability := f.guestOrder(t)

	assertFailure(t, sessionEndpoint.call(t, f, owner, userOrder, ""), xerr.InvalidAccess, "Invalid access")
	assertFailure(t, sessionEndpoint.call(t, f, guest, guestOrder, capability), xerr.OrderStatusError, "Order status error")

	// The payment callback settles the order, which dates the exchange
	// window; activation then creates the guest's account and finishes it.
	accountID, _ := f.buyer()
	if paid, err := f.h.Store.Order().MarkOrderPaid(context.Background(), guestOrder, "trade-guest"); err != nil || !paid {
		t.Fatalf("MarkOrderPaid = (%t, %v)", paid, err)
	}
	if err := f.h.DB.Model(&order.Order{}).Where("order_no = ?", guestOrder).Update("user_id", accountID).Error; err != nil {
		t.Fatal(err)
	}
	if finished, err := f.h.Store.Order().UpdateOrderStatusFrom(context.Background(), guestOrder, order.StatusPaid, order.StatusFinished); err != nil || !finished {
		t.Fatalf("finish order = (%t, %v)", finished, err)
	}
	var resp dto.V2OrderSessionResponse
	success(t, sessionEndpoint.call(t, f, guest, guestOrder, capability), &resp)
	claims, err := usersession.Validate(context.Background(), f.h.Redis, "order-handler-secret", resp.AccessToken)
	if err != nil || claims.UserID != accountID {
		t.Fatalf("access token = %q (%+v, %v), want a session of user %d", resp.AccessToken, claims, err, accountID)
	}
}
