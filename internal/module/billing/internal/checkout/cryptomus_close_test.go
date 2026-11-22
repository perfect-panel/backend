package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/cryptomus"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// cryptomusCase is a pending $10.00 Cryptomus order holding a plan unit and
// 40 of gift credit, whose invoice-1 the gateway answers through answer.
type cryptomusCase struct {
	*checkoutFixture
	buyer *user.User
	plan  *subscribe.Subscribe
}

const cryptomusOrderNo = "cryptomus-order"

func newCryptomusCase(t *testing.T, answer func() (int, string, error), adjust ...func(*order.Order)) *cryptomusCase {
	t.Helper()
	o := &order.Order{
		OrderNo: cryptomusOrderNo, Status: order.StatusPending, Type: order.TypeSubscribe, Amount: 1000, GiftAmount: 40,
		Method: "Cryptomus", PaymentCurrency: "USD", PaymentAmount: 1000, TradeNo: "invoice-1",
	}
	for _, fn := range adjust {
		fn(o)
	}
	// The invoice query goes to the production host through the injected
	// client; nothing leaves the process.
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		defer func() { _ = req.Body.Close() }()
		if req.URL.String() != cryptomus.DefaultBaseURL+"/v1/payment/info" || req.Method != http.MethodPost {
			t.Errorf("unexpected gateway request: %s %s", req.Method, req.URL)
		}
		var query map[string]string
		if err := json.NewDecoder(req.Body).Decode(&query); err != nil {
			t.Error(err)
		}
		if o.TradeNo != "" && query["uuid"] != o.TradeNo {
			t.Errorf("expected lookup by the claimed invoice, got %v", query)
		}
		if o.TradeNo == "" && query["order_id"] != o.OrderNo {
			t.Errorf("expected recovery lookup by order number, got %v", query)
		}
		status, body, err := answer()
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	f := newCheckoutFixture(t, withGateways(gateway.NewRegistry(gateway.WithHTTPClient(client))))
	buyer, _ := f.buyer(10)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 })
	method := f.h.Payment("Cryptomus", `{"merchant_id":"merchant-1","api_key":"test-key"}`)
	o.UserId, o.SubscribeId, o.PaymentId = buyer.Id, plan.Id, method.Id
	f.h.Order(o)
	if err := f.svc.deps.Inventory.Reserve(context.Background(), o.OrderNo, plan.Id); err != nil {
		t.Fatal(err)
	}
	return &cryptomusCase{checkoutFixture: f, buyer: buyer, plan: plan}
}

func (c *cryptomusCase) owner() context.Context { return billingtest.UserContext(c.buyer) }

func (c *cryptomusCase) close(ctx context.Context) error {
	return closeAs(ctx, c.svc, cryptomusOrderNo)
}

// assertReservationKept checks that the order still holds its plan unit and
// gift credit.
func (c *cryptomusCase) assertReservationKept() {
	c.t.Helper()
	if c.h.ReloadWallet(c.buyer.Id).GiftAmount != 10 || len(c.h.GiftLogs(c.buyer.Id)) != 0 || c.h.ReloadPlan(c.plan.Id).Inventory != 2 {
		c.t.Fatal("an unconfirmed or paid invoice released the order reservation")
	}
}

func cryptomusInvoice(status string, final bool, paymentAmount string) string {
	return fmt.Sprintf(`{"state":0,"result":{"uuid":"invoice-1","order_id":"cryptomus-order","amount":"10.00","currency":"USD","status":%q,"is_final":%t,"payment_amount":%q}}`, status, final, paymentAmount)
}

func answering(status int, body string) func() (int, string, error) {
	return func() (int, string, error) { return status, body, nil }
}

// Nobody's close may release an invoice that is not confirmed cancelled
// without funds.
func TestCloseCryptomusKeepsUnconfirmedInvoicesPending(t *testing.T) {
	tests := []struct {
		name, status, paid string
		final              bool
	}{
		{"waiting", "check", "0.00", false},
		{"processing", "process", "10.00", false},
		{"confirming", "confirm_check", "10.00", false},
		{"partial payment", "wrong_amount_waiting", "5.00", false},
		{"underpaid final", "wrong_amount", "5.00", true},
		{"AML locked", "locked", "10.00", true},
		{"refund pending", "refund_process", "10.00", true},
		{"refund failed", "refund_fail", "10.00", true},
		{"refund paid", "refund_paid", "10.00", true},
		{"gateway failure", "fail", "0.00", true},
		{"system failure", "system_fail", "0.00", true},
		{"unknown final", "new_status", "0.00", true},
		{"not finally cancelled", "cancel", "0.00", false},
		{"cancelled with money", "cancel", "0.001", true},
		{"cancelled missing amount", "cancel", "", true},
		{"cancelled invalid amount", "cancel", "invalid", true},
	}
	for _, test := range tests {
		for _, byOwner := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/owner=%t", test.name, byOwner), func(t *testing.T) {
				c := newCryptomusCase(t, answering(200, cryptomusInvoice(test.status, test.final, test.paid)))
				ctx := system
				if byOwner {
					ctx = c.owner()
				}
				assertUnconfirmed(t, c.close(ctx))
				if c.status(cryptomusOrderNo) != order.StatusPending || len(c.queue.Activations) != 0 {
					t.Fatal("the order did not stay pending")
				}
				c.assertReservationKept()
			})
		}
	}
}

func TestCloseCryptomusQueryErrorsNeverDiscardKnownInvoice(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
		err  error
	}{
		{"network failure", 0, "", errors.New("connection refused")},
		{"proxy 404", 404, "<html>Not found</html>", nil},
		{"merchant missing", 404, `{"state":1,"message":"Merchant not found"}`, nil},
		{"payment missing", 422, `{"state":1,"message":"Payment not found"}`, nil},
		{"server error", 500, `{"state":1,"message":"Server error"}`, nil},
	}
	for _, test := range tests {
		for _, byOwner := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/owner=%t", test.name, byOwner), func(t *testing.T) {
				c := newCryptomusCase(t, func() (int, string, error) { return test.code, test.body, test.err })
				ctx := system
				if byOwner {
					ctx = c.owner()
				}
				assertUnconfirmed(t, c.close(ctx))
				if c.status(cryptomusOrderNo) != order.StatusPending {
					t.Fatal("a query error closed the order")
				}
				c.assertReservationKept()
			})
		}
	}
}

// An invoice whose UUID was never claimed may still be being created after
// a timeout; even "payment not found" cannot close it.
func TestCloseCryptomusUnclaimedInvoiceMayStillBeCreating(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		body string
	}{
		{"currently missing payment", 422, `{"state":1,"message":"Payment not found"}`},
		{"proxy error", 404, "<html>Not found</html>"},
		{"missing merchant", 404, `{"state":1,"message":"Merchant not found"}`},
		{"generic not found", 404, `{"state":1,"message":"Not found"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCryptomusCase(t, answering(test.code, test.body), func(o *order.Order) { o.TradeNo = "" })
			assertUnconfirmed(t, c.close(system))
			if c.status(cryptomusOrderNo) != order.StatusPending {
				t.Fatal("an unconfirmed invoice was closed")
			}
			c.assertReservationKept()
		})
	}
}

func TestCloseCryptomusBeforeCheckoutCanCancel(t *testing.T) {
	c := newCryptomusCase(t, func() (int, string, error) {
		t.Error("an order before checkout needs no gateway request")
		return 0, "", errors.New("unexpected")
	}, func(o *order.Order) { o.TradeNo, o.PaymentCurrency = "", "" })
	if err := c.close(system); err != nil {
		t.Fatal(err)
	}
	if c.status(cryptomusOrderNo) != order.StatusClosed {
		t.Fatal("an order before checkout should close normally")
	}
}

// The checkout records its expectation before it creates the invoice; a
// close that read the order before that write must not close it.
func TestCloseCryptomusRechecksConcurrentCheckoutInsideTransaction(t *testing.T) {
	c := newCryptomusCase(t, func() (int, string, error) {
		t.Error("the close read the order before its checkout started")
		return 0, "", errors.New("unexpected")
	}, func(o *order.Order) { o.TradeNo, o.PaymentCurrency = "", "" })
	c.svc.deps.Tx = raceTransactor{tx: c.h.Store, compete: func() {
		if err := c.h.DB.Model(&order.Order{}).Where("order_no = ?", cryptomusOrderNo).Update("payment_currency", "USD").Error; err != nil {
			t.Fatal(err)
		}
	}}
	assertUnconfirmed(t, c.close(system))
	if c.status(cryptomusOrderNo) != order.StatusPending {
		t.Fatal("an order whose checkout just started was closed")
	}
	c.assertReservationKept()
}

func TestCloseCryptomusRequiresMatchingInvoiceBeforeCancellation(t *testing.T) {
	for _, test := range []struct{ from, to string }{
		{"invoice-1", "other-invoice"}, {"cryptomus-order", "other-order"}, {"10.00", "9.00"}, {"USD", "EUR"},
	} {
		t.Run(test.from, func(t *testing.T) {
			c := newCryptomusCase(t, answering(200, strings.Replace(cryptomusInvoice("cancel", true, "0.00"), test.from, test.to, 1)))
			assertUnconfirmed(t, c.close(system))
			c.assertReservationKept()
		})
	}
}

// An invoice cannot be cancelled, so an administrator's close waits for the
// gateway to confirm that no money was collected, like every caller.
func TestCloseByAdminKeepsCryptomusConfirmationRule(t *testing.T) {
	c := newCryptomusCase(t, answering(200, cryptomusInvoice("check", false, "0.00")))
	closed, err := c.svc.CloseByAdmin(context.Background(), cryptomusOrderNo, 99)
	if closed {
		t.Fatal("the administrator closed an unconfirmed invoice")
	}
	assertUnconfirmed(t, err)
	c.assertReservationKept()
}

func TestCloseCryptomusCancelledUnpaidInvoiceReleasesReservation(t *testing.T) {
	c := newCryptomusCase(t, answering(200, cryptomusInvoice("cancel", true, "0.00000000")))
	if err := c.close(system); err != nil {
		t.Fatal(err)
	}
	if c.status(cryptomusOrderNo) != order.StatusClosed || c.h.ReloadWallet(c.buyer.Id).GiftAmount != 50 || c.h.ReloadPlan(c.plan.Id).Inventory != 3 {
		t.Fatal("a verified cancellation must close and release the reservation")
	}
}

func TestCloseCryptomusSettlesAfterRefusingEarlyUserCancellation(t *testing.T) {
	for _, paidStatus := range []string{"paid", "paid_over"} {
		t.Run(paidStatus, func(t *testing.T) {
			status := "confirm_check"
			c := newCryptomusCase(t, func() (int, string, error) {
				return 200, cryptomusInvoice(status, status != "confirm_check", "10.00"), nil
			})
			assertUnconfirmed(t, c.close(c.owner()))
			status = paidStatus
			if err := c.close(system); err != nil {
				t.Fatalf("the reconciler must settle the late payment: %v", err)
			}
			if c.status(cryptomusOrderNo) != order.StatusPaid || len(c.queue.Activations) != 1 {
				t.Fatal("a paid invoice must activate exactly once instead of closing")
			}
			c.assertReservationKept()
		})
	}
}
