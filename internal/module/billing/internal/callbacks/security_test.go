package callbacks

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	"github.com/perfect-panel/backend/pkg/xerr"
	stripeSDK "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
)

// callbackOrders holds one order and counts the Pending->Paid transitions.
type callbackOrders struct {
	order     *order.Order
	markCount int
}

var _ settle.Orders = (*callbackOrders)(nil)

func (r *callbackOrders) FindOneByOrderNo(_ context.Context, orderNo string) (*order.Order, error) {
	if r.order == nil || r.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.order
	return &copy, nil
}

func (*callbackOrders) ReservePaymentCapture(context.Context, string, string) (bool, error) {
	panic("unexpected outbound capture")
}

func (r *callbackOrders) MarkOrderPaid(_ context.Context, orderNo, tradeNo string) (bool, error) {
	if r.order.OrderNo != orderNo || r.order.Status != order.StatusPending {
		return false, nil
	}
	r.order.Status = order.StatusPaid
	r.order.TradeNo = tradeNo
	r.markCount++
	return true, nil
}

type fakeActivationQueue struct {
	enqueued []string
}

func (f *fakeActivationQueue) EnqueueActivation(_ context.Context, orderNo string) error {
	f.enqueued = append(f.enqueued, orderNo)
	return nil
}

func paymentContext(method *payment.Payment) context.Context {
	return context.WithValue(context.Background(), requestctx.CtxKeyPayment, method)
}

// pendingOrder is order-1, the order the callbacks name, pending payment of
// amount in currency through method.
func pendingOrder(method *payment.Payment, amount int64, currency string) *order.Order {
	return &order.Order{
		OrderNo: "order-1", PaymentId: method.Id, Method: method.Platform, Status: order.StatusPending,
		PaymentAmount: amount, PaymentCurrency: currency,
	}
}

func TestNotifyWithoutPaymentConfigIsRejected(t *testing.T) {
	err := NewService(nil, nil, nil).Notify(context.Background(), gateway.Notification{})
	if err == nil || !strings.Contains(err.Error(), "payment config not found") {
		t.Fatalf("Notify = %v", err)
	}
}

func TestNotifyRejectsPlatformsWithoutGateway(t *testing.T) {
	for _, platform := range []string{"CryptoSaaS", "balance"} {
		err := NewService(nil, nil, nil).Notify(paymentContext(&payment.Payment{Platform: platform}), gateway.Notification{})
		if !errors.Is(err, gateway.ErrNotGateway) {
			t.Fatalf("%s: Notify = %v, want ErrNotGateway", platform, err)
		}
	}
}

// ---------------------------------------------------------------- EPay

func epayMethod(queryURL string) *payment.Payment {
	return &payment.Payment{
		Id: 10, Platform: "EPay",
		Config: `{"pid":"1001","url":"` + queryURL + `","key":"secret","type":"alipay"}`,
	}
}

// signedEPayParams is the signed EPay callback reporting order-1 paid with
// 10.00.
func signedEPayParams() map[string]string {
	params := map[string]string{
		"pid": "1001", "trade_no": "trade-1", "out_trade_no": "order-1", "type": "alipay",
		"name": "product", "money": "10.00", "trade_status": "TRADE_SUCCESS", "param": "", "sign_type": "MD5",
	}
	params["sign"] = signEPayTestParams(params, "secret")
	return params
}

func epayQueryServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

func TestEPayNotifyRejectsInvalidSignature(t *testing.T) {
	method := epayMethod("https://pay.example")
	svc := NewService(&callbackOrders{}, nil, nil)
	err := svc.Notify(paymentContext(method), gateway.Notification{HTTPMethod: "POST", Params: map[string]string{
		"out_trade_no": "order-1", "trade_status": "TRADE_SUCCESS", "sign": "invalid",
	}})
	if err == nil || !strings.Contains(err.Error(), "verify sign failed") {
		t.Fatalf("an invalid signature must be rejected, got %v", err)
	}
}

func TestEPayNotifySettlesOnlyAfterSignedAndQueriedDetailsMatch(t *testing.T) {
	queryURL := epayQueryServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 1, "pid": "1001", "trade_no": "trade-1", "out_trade_no": "order-1",
			"type": "alipay", "money": "10.00", "status": 1,
		})
	})
	method := epayMethod(queryURL)
	orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
	queue := &fakeActivationQueue{}
	svc := NewService(orders, queue, nil)
	notification := gateway.Notification{HTTPMethod: "POST", Params: signedEPayParams()}

	for range 2 {
		if err := svc.Notify(paymentContext(method), notification); err != nil {
			t.Fatalf("Notify: %v", err)
		}
	}
	if orders.markCount != 1 || orders.order.Status != order.StatusPaid || orders.order.TradeNo != "trade-1" {
		t.Fatalf("order was not settled exactly once: %+v, marks=%d", orders.order, orders.markCount)
	}
	if len(queue.enqueued) == 0 {
		t.Fatal("settlement must enqueue activation")
	}
}

func TestEPayNotifySettlesWithSignedCallbackWhenQueryUnsupported(t *testing.T) {
	method := epayMethod(epayQueryServer(t, http.NotFound))
	orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
	svc := NewService(orders, &fakeActivationQueue{}, nil)

	if err := svc.Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if orders.markCount != 1 || orders.order.TradeNo != "trade-1" {
		t.Fatalf("signed fallback callback did not settle order: %+v", orders.order)
	}
}

// A gateway whose query only reports a status confirms the payment with it;
// the fields it omits were verified in the signed callback.
func TestEPayNotifyAcceptsAPaidStatusOnlyQuery(t *testing.T) {
	method := epayMethod(epayQueryServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api.php") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"status":"success"}}`))
	}))
	orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
	if err := NewService(orders, &fakeActivationQueue{}, nil).Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if orders.markCount != 1 {
		t.Fatal("a paid status-only confirmation must settle the order")
	}
}

func TestEPayNotifyRejectsMismatchedCallbacks(t *testing.T) {
	method := epayMethod(epayQueryServer(t, http.NotFound))
	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"amount below the expectation", func(p map[string]string) { p["money"] = "9.99" }, "amount mismatch"},
		{"other merchant", func(p map[string]string) { p["pid"] = "2002" }, "merchant id mismatch"},
		{"other payment type", func(p map[string]string) { p["type"] = "wxpay" }, "payment type mismatch"},
		{"unfinished trade", func(p map[string]string) { p["trade_status"] = "WAIT_BUYER_PAY" }, "not success"},
		{"unknown order", func(p map[string]string) { p["out_trade_no"] = "order-2" }, "order not exist"},
		{"malformed money", func(p map[string]string) { p["money"] = "10.001" }, "invalid callback money"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
			params := signedEPayParams()
			delete(params, "sign")
			tt.mutate(params)
			params["sign"] = signEPayTestParams(params, "secret")
			err := NewService(orders, &fakeActivationQueue{}, nil).Notify(paymentContext(method), gateway.Notification{Params: params})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Notify = %v, want %q", err, tt.want)
			}
			if orders.markCount != 0 {
				t.Fatal("a mismatched callback settled the order")
			}
		})
	}
}

// The gateway's own record must agree with the signed callback.
func TestEPayNotifyRejectsWhenTheGatewayDisagrees(t *testing.T) {
	for name, body := range map[string]string{
		"unpaid":         `{"code":1,"pid":"1001","trade_no":"trade-1","out_trade_no":"order-1","type":"alipay","money":"10.00","status":0}`,
		"other amount":   `{"code":1,"pid":"1001","trade_no":"trade-1","out_trade_no":"order-1","type":"alipay","money":"1.00","status":1}`,
		"other merchant": `{"code":1,"pid":"other","trade_no":"trade-1","out_trade_no":"order-1","type":"alipay","money":"10.00","status":1}`,
		"other trade":    `{"code":1,"pid":"1001","trade_no":"trade-2","out_trade_no":"order-1","type":"alipay","money":"10.00","status":1}`,
		"lookup failed":  `{"code":-1,"msg":"order not found"}`,
	} {
		t.Run(name, func(t *testing.T) {
			method := epayMethod(epayQueryServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
			if err := NewService(orders, &fakeActivationQueue{}, nil).Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err == nil {
				t.Fatal("the callback was accepted although the gateway disagrees")
			}
			if orders.markCount != 0 {
				t.Fatal("the order was settled")
			}
		})
	}
}

// ---------------------------------------------------------------- Stripe

// fakeStripeIntents answers PaymentIntent reads with a fixed status.
type fakeStripeIntents struct {
	mu     sync.Mutex
	status string
	reads  int
}

func (f *fakeStripeIntents) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"id":%q,"object":"payment_intent","status":%q}`, strings.TrimPrefix(r.URL.Path, "/v1/payment_intents/"), f.status)
}

func stripeFixture(t *testing.T, status string) (*gateway.Registry, *fakeStripeIntents, *payment.Payment) {
	t.Helper()
	fake := &fakeStripeIntents{status: status}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	registry := gateway.NewRegistry(gateway.WithStripeBackends(stripeSDK.NewBackendsWithConfig(&stripeSDK.BackendConfig{
		URL: stripeSDK.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripeSDK.Int64(0),
		LeveledLogger: &stripeSDK.LeveledLogger{Level: stripeSDK.LevelNull},
	})))
	method := &payment.Payment{Id: 20, Platform: "Stripe", Config: `{"secret_key":"sk_test","webhook_secret":"whsec_test","payment":"card"}`}
	return registry, fake, method
}

func stripeEvent(eventType string, amount int64, currency, method string) gateway.Notification {
	payload := []byte(fmt.Sprintf(`{"id":"evt_1","object":"event","type":%q,"api_version":"2024-04-10","data":{"object":{"id":"pi_1","object":"payment_intent","amount_received":%d,"currency":%q,"metadata":{"order_no":"order-1","user_id":"7"},"payment_method_types":[%q]}}}`,
		eventType, amount, currency, method))
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: "whsec_test", Timestamp: time.Now()})
	return gateway.Notification{Body: signed.Payload, Signature: signed.Header}
}

func TestStripeNotifySettlesAConfirmedIntent(t *testing.T) {
	registry, fake, method := stripeFixture(t, "succeeded")
	orders := &callbackOrders{order: pendingOrder(method, 1990, "USD")}
	orders.order.TradeNo = "pi_1"
	svc := NewService(orders, &fakeActivationQueue{}, registry)

	if err := svc.Notify(paymentContext(method), stripeEvent("payment_intent.succeeded", 1990, "usd", "card")); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if orders.markCount != 1 || fake.reads != 1 {
		t.Fatalf("marks=%d intent reads=%d, want the intent confirmed and the order settled", orders.markCount, fake.reads)
	}
}

func TestStripeNotifyRequiresBoundAmountCurrencyAndMethod(t *testing.T) {
	tests := []struct {
		name       string
		event      gateway.Notification
		want       string
		wantStatus string
	}{
		{"amount", stripeEvent("payment_intent.succeeded", 999, "usd", "card"), "amount mismatch", "succeeded"},
		{"currency", stripeEvent("payment_intent.succeeded", 1990, "eur", "card"), "currency mismatch", "succeeded"},
		{"method", stripeEvent("payment_intent.succeeded", 1990, "usd", "wechat_pay"), "payment method mismatch", "succeeded"},
		{"not paid at Stripe", stripeEvent("payment_intent.succeeded", 1990, "usd", "card"), "not paid", "processing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, _, method := stripeFixture(t, tt.wantStatus)
			orders := &callbackOrders{order: pendingOrder(method, 1990, "USD")}
			err := NewService(orders, &fakeActivationQueue{}, registry).Notify(paymentContext(method), tt.event)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Notify = %v, want %q", err, tt.want)
			}
			if orders.markCount != 0 {
				t.Fatal("the order was settled")
			}
		})
	}
}

// Events other than a succeeded intent are acknowledged without touching
// the order; a forged signature is rejected.
func TestStripeNotifyAcknowledgesOtherEventsAndRejectsForgeries(t *testing.T) {
	registry, fake, method := stripeFixture(t, "succeeded")
	orders := &callbackOrders{}
	svc := NewService(orders, &fakeActivationQueue{}, registry)
	if err := svc.Notify(paymentContext(method), stripeEvent("payment_intent.payment_failed", 1990, "usd", "card")); err != nil {
		t.Fatalf("a lifecycle event must be acknowledged: %v", err)
	}
	forged := stripeEvent("payment_intent.succeeded", 1990, "usd", "card")
	forged.Signature = "t=1,v1=forged"
	if err := svc.Notify(paymentContext(method), forged); err == nil {
		t.Fatal("a forged webhook was accepted")
	}
	if fake.reads != 0 || orders.markCount != 0 {
		t.Fatal("unauthenticated or irrelevant events reached Stripe or the order")
	}
}

// ---------------------------------------------------------------- Alipay

// alipayTradeServer answers alipay.trade.query with a signed trade status.
func alipayTradeServer(t *testing.T, status string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		biz := fmt.Sprintf(`{"code":"10000","msg":"Success","trade_no":"trade-1","out_trade_no":"order-1","trade_status":%q,"total_amount":"10.00"}`, status)
		_, _ = fmt.Fprintf(w, `{"alipay_trade_query_response":%s,"sign":%q}`, biz, billingtest.AlipaySign(t, biz))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func alipayMethod(t *testing.T, gatewayURL string) *payment.Payment {
	t.Helper()
	return &payment.Payment{Id: 30, Platform: "AlipayF2F", Config: billingtest.AlipayConfig(t, "app-1", gatewayURL)}
}

// alipayNotification signs the notification form the way the gateway does:
// the sorted key=value pairs without sign and sign_type.
func alipayNotification(t *testing.T, fields map[string]string) gateway.Notification {
	t.Helper()
	form := url.Values{}
	pairs := make([]string, 0, len(fields))
	for key, value := range fields {
		form.Set(key, value)
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)
	form.Set("sign", billingtest.AlipaySign(t, strings.Join(pairs, "&")))
	form.Set("sign_type", "RSA2")
	return gateway.Notification{Form: form}
}

func alipayFields(overrides map[string]string) map[string]string {
	fields := map[string]string{
		"app_id": "app-1", "out_trade_no": "order-1", "trade_no": "trade-1",
		"trade_status": "TRADE_SUCCESS", "total_amount": "10.00", "notify_id": "n-1",
	}
	for key, value := range overrides {
		fields[key] = value
	}
	return fields
}

func TestAlipayNotifySettlesAConfirmedTrade(t *testing.T) {
	method := alipayMethod(t, alipayTradeServer(t, "TRADE_SUCCESS"))
	orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
	if err := NewService(orders, &fakeActivationQueue{}, nil).Notify(paymentContext(method), alipayNotification(t, alipayFields(nil))); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if orders.markCount != 1 || orders.order.TradeNo != "trade-1" {
		t.Fatalf("order = %+v, want settled", orders.order)
	}
}

func TestAlipayNotifyRequiresBoundAppAndExactAmount(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		status    string
		want      string
	}{
		{"other app", map[string]string{"app_id": "other-app"}, "TRADE_SUCCESS", "app id mismatch"},
		{"other amount", map[string]string{"total_amount": "9.99"}, "TRADE_SUCCESS", "amount mismatch"},
		{"unpaid at the gateway", nil, "WAIT_BUYER_PAY", "not paid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := alipayMethod(t, alipayTradeServer(t, tt.status))
			orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
			err := NewService(orders, &fakeActivationQueue{}, nil).Notify(paymentContext(method), alipayNotification(t, alipayFields(tt.overrides)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Notify = %v, want %q", err, tt.want)
			}
			if orders.markCount != 0 {
				t.Fatal("the order was settled")
			}
		})
	}
}

func TestAlipayNotifyAcknowledgesUnpaidTradesAndRejectsForgeries(t *testing.T) {
	method := alipayMethod(t, "http://127.0.0.1:1")
	orders := &callbackOrders{}
	svc := NewService(orders, &fakeActivationQueue{}, nil)
	if err := svc.Notify(paymentContext(method), alipayNotification(t, alipayFields(map[string]string{"trade_status": "TRADE_CLOSED"}))); err != nil {
		t.Fatalf("a closed trade notification must be acknowledged: %v", err)
	}
	forged := alipayNotification(t, alipayFields(nil))
	forged.Form.Set("total_amount", "0.01")
	if err := svc.Notify(paymentContext(method), forged); err == nil {
		t.Fatal("a tampered notification was accepted")
	}
}

// ---------------------------------------------------------------- shared rules

func TestValidateOrderPaymentRequiresExactConfigurationBinding(t *testing.T) {
	method := &payment.Payment{Id: 10, Platform: "EPay"}
	if err := validateOrderPayment(&order.Order{PaymentId: 10, Method: "EPay"}, method); err != nil {
		t.Fatalf("matching payment binding rejected: %v", err)
	}
	if err := validateOrderPayment(&order.Order{PaymentId: 11, Method: "EPay"}, method); err == nil {
		t.Fatal("mismatched payment id must be rejected")
	}
	if err := validateOrderPayment(&order.Order{PaymentId: 10, Method: "Stripe"}, method); err == nil {
		t.Fatal("mismatched payment platform must be rejected")
	}
}

func TestValidatePaymentExpectationRequiresAmountAndCurrency(t *testing.T) {
	orderInfo := &order.Order{PaymentAmount: 1000, PaymentCurrency: "CNY"}
	if err := validatePaymentExpectation(orderInfo, 1000, "cny"); err != nil {
		t.Fatalf("matching expectation rejected: %v", err)
	}
	if err := validatePaymentExpectation(orderInfo, 999, "CNY"); err == nil {
		t.Fatal("amount mismatch must be rejected")
	}
	if err := validatePaymentExpectation(orderInfo, 1000, "USD"); err == nil {
		t.Fatal("currency mismatch must be rejected")
	}
	if err := validatePaymentExpectation(&order.Order{PaymentAmount: 1000}, 1000, "CNY"); err == nil {
		t.Fatal("missing checkout snapshot must fail closed")
	}
}

func TestActivationTaskIDIsDeterministicPerOrder(t *testing.T) {
	first := taskqueue.ActivationTaskID("order-1")
	if first != taskqueue.ActivationTaskID("order-1") {
		t.Fatal("activation task id must be deterministic")
	}
	if first == taskqueue.ActivationTaskID("order-2") {
		t.Fatal("different orders must not share an activation task id")
	}
}

func TestFinishedOrderDuplicateRequiresSameTradeNumber(t *testing.T) {
	ctx := context.Background()
	orderInfo := &order.Order{Status: order.StatusFinished, TradeNo: "trade-1"}
	finished, err := finishedOrderDuplicate(ctx, orderInfo, "trade-1")
	if err != nil || !finished {
		t.Fatalf("matching finished duplicate rejected: finished=%t err=%v", finished, err)
	}
	if _, err := finishedOrderDuplicate(ctx, orderInfo, "trade-2"); err == nil {
		t.Fatal("finished callback with another trade number must be rejected")
	}
}

// Historical orders completed before trade_no persistence was introduced are
// treated as safe duplicates rather than blocking the payment callback.
func TestFinishedOrderDuplicateToleratesEmptyTradeNo(t *testing.T) {
	finished, err := finishedOrderDuplicate(context.Background(), &order.Order{Status: order.StatusFinished}, "trade-abc")
	if err != nil || !finished {
		t.Fatalf("legacy finished order = (%t, %v), want a tolerated duplicate", finished, err)
	}
}

func TestCancelledOrFailedOrderCannotSettle(t *testing.T) {
	for _, status := range []uint8{order.StatusClosed, order.StatusFailed} {
		if err := validateOrderCanSettle(&order.Order{Status: status}); err == nil {
			t.Fatalf("status %d must not be settled", status)
		}
	}
}

func signEPayTestParams(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if value != "" && name != "sign" && name != "sign_type" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		parts = append(parts, name+"="+params[name])
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(digest[:])
}

// A rejection redelivery cannot fix (a forged signature, an unknown order, an
// amount that is not the order's) is marked ErrInvalidCallback, so the notify
// route answers Stripe with 400; a failure the next delivery may get past
// (the order closed meanwhile, Stripe unreachable) is not, so Stripe retries.
// The error text the other gateways receive as their failure body is kept.
func TestStripeNotifyClassifiesRejections(t *testing.T) {
	registry, _, method := stripeFixture(t, "succeeded")
	forged := stripeEvent("payment_intent.succeeded", 1990, "usd", "card")
	forged.Signature = "t=1,v1=forged"
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()
	offline := gateway.NewRegistry(gateway.WithStripeBackends(stripeSDK.NewBackendsWithConfig(&stripeSDK.BackendConfig{
		URL: stripeSDK.String(unreachable.URL), HTTPClient: unreachable.Client(), MaxNetworkRetries: stripeSDK.Int64(0),
		LeveledLogger: &stripeSDK.LeveledLogger{Level: stripeSDK.LevelNull},
	})))
	closed := pendingOrder(method, 1990, "USD")
	closed.Status = order.StatusClosed
	tests := []struct {
		name     string
		registry *gateway.Registry
		orders   *callbackOrders
		event    gateway.Notification
		invalid  bool
	}{
		{"forged signature", registry, &callbackOrders{}, forged, true},
		{"unknown order", registry, &callbackOrders{}, stripeEvent("payment_intent.succeeded", 1990, "usd", "card"), true},
		{"amount mismatch", registry, &callbackOrders{order: pendingOrder(method, 1990, "USD")}, stripeEvent("payment_intent.succeeded", 999, "usd", "card"), true},
		{"order closed meanwhile", registry, &callbackOrders{order: closed}, stripeEvent("payment_intent.succeeded", 1990, "usd", "card"), false},
		{"Stripe unreachable", offline, &callbackOrders{order: pendingOrder(method, 1990, "USD")}, stripeEvent("payment_intent.succeeded", 1990, "usd", "card"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewService(tt.orders, &fakeActivationQueue{}, tt.registry).Notify(paymentContext(method), tt.event)
			if err == nil {
				t.Fatal("the callback was accepted")
			}
			if errors.Is(err, gateway.ErrInvalidCallback) != tt.invalid {
				t.Fatalf("Notify = %v; invalid = %t, want %t", err, !tt.invalid, tt.invalid)
			}
			if tt.orders.markCount != 0 {
				t.Fatal("the order was settled")
			}
		})
	}
	marked := gateway.InvalidCallback(errors.New("verify sign failed"))
	if marked.Error() != "verify sign failed" || xerr.CodeOf(gateway.InvalidCallback(xerr.Errorf(xerr.OrderNotExist, "order not exist"))) != xerr.OrderNotExist {
		t.Fatal("marking a callback invalid changed its text or hid its code")
	}
}
