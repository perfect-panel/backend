package callbacks

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
)

func signCryptomusTestPayload(t *testing.T, apiKey string, fields map[string]any) []byte {
	t.Helper()
	unsigned, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	digest := md5.Sum([]byte(base64.StdEncoding.EncodeToString(unsigned) + apiKey))
	sign := hex.EncodeToString(digest[:])
	return []byte(strings.TrimSuffix(string(unsigned), "}") + `,"sign":"` + sign + `"}`)
}

func cryptomusPaidNotification(t *testing.T, apiKey string) []byte {
	t.Helper()
	return signCryptomusTestPayload(t, apiKey, map[string]any{
		"type": "payment", "uuid": "uuid-1", "order_id": "order-1",
		"amount": "10.00", "currency": "USD", "payer_currency": "USDT",
		"status": "paid", "is_final": true,
	})
}

func cryptomusInfoServer(t *testing.T, invoice string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/payment/info" {
			t.Errorf("unexpected gateway path %s", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, invoice)
	}))
}

// The Cryptomus method the orders are bound to: its id and the API key its
// callbacks are signed with.
const (
	cryptomusMethodID int64 = 11
	cryptomusAPIKey         = "api-key"
)

// cryptomusPaymentConfig is the configured Cryptomus method.
func cryptomusPaymentConfig() *payment.Payment {
	return &payment.Payment{
		Id:       cryptomusMethodID,
		Platform: "Cryptomus",
		Config:   `{"merchant_id":"merchant-1","api_key":"` + cryptomusAPIKey + `"}`,
	}
}

// cryptomusRegistry points the confirmation query at a test stub. The base
// URL is a registry option only tests set; it is not part of the database
// payment configuration.
func cryptomusRegistry(url string) *gateway.Registry {
	return gateway.NewRegistry(gateway.WithCryptomusBaseURL(url))
}

func cryptomusNotification(payload []byte) gateway.Notification {
	return gateway.Notification{HTTPMethod: "POST", Body: payload}
}

func TestCryptomusNotifySettlesOnlyAfterSignedAndQueriedInvoiceMatch(t *testing.T) {
	queryServer := cryptomusInfoServer(t,
		`{"state":0,"result":{"uuid":"uuid-1","order_id":"order-1","amount":"10.00","currency":"USD","payment_status":"paid","is_final":true}}`)
	defer queryServer.Close()

	queue := &fakeActivationQueue{}
	orders := &callbackOrders{order: &order.Order{
		OrderNo: "order-1", PaymentId: cryptomusMethodID, Method: "Cryptomus", Status: order.StatusPending,
		PaymentAmount: 1000, PaymentCurrency: "USD",
	}}
	ctx := paymentContext(cryptomusPaymentConfig())
	svc := NewService(orders, queue, cryptomusRegistry(queryServer.URL))
	payload := cryptomusNotification(cryptomusPaidNotification(t, cryptomusAPIKey))

	if err := svc.Notify(ctx, payload); err != nil {
		t.Fatalf("CryptomusNotify: %v", err)
	}
	if err := svc.Notify(ctx, payload); err != nil {
		t.Fatalf("duplicate CryptomusNotify must be idempotent: %v", err)
	}
	if orders.markCount != 1 || orders.order.Status != order.StatusPaid || orders.order.TradeNo != "uuid-1" {
		t.Fatalf("order was not settled exactly once: %+v, marks=%d", orders.order, orders.markCount)
	}
	if len(queue.enqueued) == 0 {
		t.Fatal("settlement must enqueue activation")
	}
}

func TestCryptomusNotifyRejectsInvalidSignature(t *testing.T) {
	ctx := paymentContext(cryptomusPaymentConfig())
	svc := NewService(nil, nil, nil)

	payload := cryptomusPaidNotification(t, "wrong-key")
	if err := svc.Notify(ctx, cryptomusNotification(payload)); err == nil || !strings.Contains(err.Error(), "verify sign failed") {
		t.Fatalf("payload signed with another key must be rejected, got %v", err)
	}

	tampered := []byte(strings.Replace(string(cryptomusPaidNotification(t, cryptomusAPIKey)), `"10.00"`, `"1.00"`, 1))
	if err := svc.Notify(ctx, cryptomusNotification(tampered)); err == nil || !strings.Contains(err.Error(), "verify sign failed") {
		t.Fatalf("tampered payload must be rejected, got %v", err)
	}
}

func TestCryptomusNotifyRejectsWrongTypeAndAmountMismatch(t *testing.T) {
	orders := &callbackOrders{order: &order.Order{
		OrderNo: "order-1", PaymentId: cryptomusMethodID, Method: "Cryptomus", Status: order.StatusPending,
		PaymentAmount: 1000, PaymentCurrency: "USD",
	}}
	ctx := paymentContext(cryptomusPaymentConfig())
	svc := NewService(orders, nil, nil)

	walletTopup := signCryptomusTestPayload(t, cryptomusAPIKey, map[string]any{
		"type": "wallet", "uuid": "uuid-1", "order_id": "order-1",
		"amount": "10.00", "currency": "USD", "status": "paid", "is_final": true,
	})
	if err := svc.Notify(ctx, cryptomusNotification(walletTopup)); err == nil || !strings.Contains(err.Error(), "notification type") {
		t.Fatalf("wallet webhooks must not settle orders, got %v", err)
	}

	underpaid := signCryptomusTestPayload(t, cryptomusAPIKey, map[string]any{
		"type": "payment", "uuid": "uuid-1", "order_id": "order-1",
		"amount": "9.00", "currency": "USD", "status": "paid", "is_final": true,
	})
	if err := svc.Notify(ctx, cryptomusNotification(underpaid)); err == nil || !strings.Contains(err.Error(), "amount mismatch") {
		t.Fatalf("amount below the payment expectation must be rejected, got %v", err)
	}

	wrongCurrency := signCryptomusTestPayload(t, cryptomusAPIKey, map[string]any{
		"type": "payment", "uuid": "uuid-1", "order_id": "order-1",
		"amount": "10.00", "currency": "EUR", "status": "paid", "is_final": true,
	})
	if err := svc.Notify(ctx, cryptomusNotification(wrongCurrency)); err == nil || !strings.Contains(err.Error(), "currency mismatch") {
		t.Fatalf("currency mismatch must be rejected, got %v", err)
	}
}

func TestCryptomusNotifyAcknowledgesNonPaidStatusesWithoutSettlement(t *testing.T) {
	statuses := []string{"check", "process", "confirm_check", "wrong_amount_waiting", "wrong_amount",
		"cancel", "fail", "system_fail", "locked", "refund_process", "refund_fail", "refund_paid"}
	for _, status := range statuses {
		for _, localStatus := range []uint8{order.StatusPending, order.StatusPaid, order.StatusFinished, 3} {
			t.Run(fmt.Sprintf("%s/local=%d", status, localStatus), func(t *testing.T) {
				orders := &callbackOrders{order: &order.Order{
					OrderNo: "order-1", TradeNo: "uuid-1", PaymentId: cryptomusMethodID, Method: "Cryptomus", Status: localStatus,
					PaymentAmount: 1000, PaymentCurrency: "USD",
				}}
				queue := &fakeActivationQueue{}
				// Non-settling events must not query the gateway or enqueue work.
				svc := NewService(orders, queue, cryptomusRegistry(":invalid-gateway"))
				ctx := paymentContext(cryptomusPaymentConfig())
				payload := signCryptomusTestPayload(t, cryptomusAPIKey, map[string]any{
					"type": "payment", "uuid": "uuid-1", "order_id": "order-1",
					"amount": "10.00000000", "currency": "USD", "status": status,
				})
				for range 2 {
					if err := svc.Notify(ctx, cryptomusNotification(payload)); err != nil {
						t.Fatalf("valid lifecycle event must be acknowledged: %v", err)
					}
				}
				if orders.order.Status != localStatus || orders.markCount != 0 || len(queue.enqueued) != 0 {
					t.Fatalf("non-paid event mutated the order: %+v", orders.order)
				}
			})
		}
	}
}

func TestCryptomusNonPaidNotificationStillRequiresAuthenticationAndBinding(t *testing.T) {
	tests := []struct {
		name, field, value, want string
	}{
		{"bad signature", "sign", strings.Repeat("0", 32), "verify sign failed"},
		{"another invoice", "uuid", "uuid-2", "trade number mismatch"},
		{"another order", "order_id", "other-order", "order not exist"},
		{"wrong amount", "amount", "9.00", "amount mismatch"},
		{"wrong currency", "currency", "EUR", "currency mismatch"},
		{"wrong type", "type", "wallet", "notification type"},
		{"unknown status", "status", "invented_status", "unknown payment status"},
		{"empty status", "status", "", "unknown payment status"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			orders := &callbackOrders{order: &order.Order{
				OrderNo: "order-1", TradeNo: "uuid-1", PaymentId: cryptomusMethodID, Method: "Cryptomus", Status: order.StatusPending,
				PaymentAmount: 1000, PaymentCurrency: "USD",
			}}
			fields := map[string]any{
				"type": "payment", "uuid": "uuid-1", "order_id": "order-1",
				"amount": "10.00", "currency": "USD", "status": "confirm_check",
			}
			fields[test.field] = test.value
			ctx := paymentContext(cryptomusPaymentConfig())
			err := NewService(orders, nil, nil).Notify(ctx, cryptomusNotification(signCryptomusTestPayload(t, cryptomusAPIKey, fields)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if orders.markCount != 0 || orders.order.Status != order.StatusPending {
				t.Fatal("rejected notification changed the order")
			}
		})
	}
}

func TestCryptomusNotifyRejectsWhenGatewayDisagrees(t *testing.T) {
	tests := []struct {
		name    string
		invoice string
		want    string
	}{
		{
			name:    "unpaid at gateway",
			invoice: `{"state":0,"result":{"uuid":"uuid-1","order_id":"order-1","amount":"10.00","currency":"USD","status":"process","is_final":false}}`,
			want:    "not paid",
		},
		{
			name:    "identity mismatch",
			invoice: `{"state":0,"result":{"uuid":"uuid-2","order_id":"order-1","amount":"10.00","currency":"USD","status":"paid","is_final":true}}`,
			want:    "identity mismatch",
		},
		{
			name:    "amount mismatch",
			invoice: `{"state":0,"result":{"uuid":"uuid-1","order_id":"order-1","amount":"9.00","currency":"USD","status":"paid","is_final":true}}`,
			want:    "amount mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryServer := cryptomusInfoServer(t, test.invoice)
			defer queryServer.Close()

			orders := &callbackOrders{order: &order.Order{
				OrderNo: "order-1", PaymentId: cryptomusMethodID, Method: "Cryptomus", Status: order.StatusPending,
				PaymentAmount: 1000, PaymentCurrency: "USD",
			}}
			ctx := paymentContext(cryptomusPaymentConfig())
			svc := NewService(orders, &fakeActivationQueue{}, cryptomusRegistry(queryServer.URL))

			err := svc.Notify(ctx, cryptomusNotification(cryptomusPaidNotification(t, cryptomusAPIKey)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
			if orders.markCount != 0 {
				t.Fatal("order must not settle when the gateway disagrees")
			}
		})
	}
}

func TestCryptomusNotifyRequiresExactPaymentBinding(t *testing.T) {
	orders := &callbackOrders{order: &order.Order{
		OrderNo: "order-1", PaymentId: 12, Method: "EPay", Status: order.StatusPending,
		PaymentAmount: 1000, PaymentCurrency: "USD",
	}}
	ctx := paymentContext(cryptomusPaymentConfig())
	svc := NewService(orders, nil, nil)

	err := svc.Notify(ctx, cryptomusNotification(cryptomusPaidNotification(t, cryptomusAPIKey)))
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("order bound to another payment method must be rejected, got %v", err)
	}
}
