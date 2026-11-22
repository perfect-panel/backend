package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// unconfirmedCloseService refuses every close: the gateway has not
// confirmed the order's payment yet.
type unconfirmedCloseService struct{}

var _ OrderCloser = unconfirmedCloseService{}

func (unconfirmedCloseService) CloseOrder(context.Context, *dto.CloseOrderRequest) error {
	return fmt.Errorf("gateway still confirming: %w", billing.ErrGatewayUnconfirmed)
}

func TestCloseOrderReportsUnconfirmedPaymentConflict(t *testing.T) {
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.SetBodyString(`{"orderNo":"order-1"}`)
	CloseOrderHandler(unconfirmedCloseService{})(context.Background(), ctx)
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatal(err)
	}
	if ctx.Response.StatusCode() != http.StatusOK || response.Code != int(xerr.PaymentStatusUnconfirmed) || response.Message != "PAYMENT_STATUS_UNCONFIRMED" {
		t.Fatalf("unexpected conflict response: %s", ctx.Response.Body())
	}
}

// closerFunc serves the OrderCloser port with a function.
type closerFunc func(context.Context, *dto.CloseOrderRequest) error

func (f closerFunc) CloseOrder(ctx context.Context, req *dto.CloseOrderRequest) error {
	return f(ctx, req)
}

// closeBody is the close request of orderNo.
func closeBody(orderNo string) string { return `{"orderNo":"` + orderNo + `"}` }

// serveClose posts body to the close route served by closer.
func serveClose(closer OrderCloser, body string) *ut.ResponseRecorder {
	engine := server.New()
	engine.POST("/v1/public/order/close", CloseOrderHandler(closer))
	return ut.PerformRequest(engine.Engine, http.MethodPost, "/v1/public/order/close",
		&ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
}

// pendingOrder places a pending EPay purchase for account through the V1
// checkout and returns its number.
func pendingOrder(t *testing.T, f *orderFacade, account context.Context) string {
	t.Helper()
	plan := f.h.Plan(1000)
	method := f.h.Payment("EPay", epayConfig)
	purchase, err := f.svc.Purchase(account, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	return purchase.OrderNo
}

// A buyer abandoning a pending order closes it: the order is closed and its
// reserved plan unit returned. Closing it again, or closing an order that
// does not exist, succeeds without effect, so a retried request is safe.
func TestCloseOrderHandlerClosesTheBuyersPendingOrder(t *testing.T) {
	f := newOrderFacade(t)
	_, owner := f.buyer()
	orderNo := pendingOrder(t, f, owner)

	success(t, f.serve(owner, http.MethodPost, "/v1/public/order/close", closeBody(orderNo)), nil)
	if closed := f.h.ReloadOrder(orderNo); closed.Status != order.StatusClosed {
		t.Fatalf("order status = %d, want closed", closed.Status)
	}
	if len(f.stock.restored) != 1 || f.stock.restored[0] != orderNo {
		t.Fatalf("restored = %v, want the order's plan unit", f.stock.restored)
	}

	success(t, f.serve(owner, http.MethodPost, "/v1/public/order/close", closeBody(orderNo)), nil)
	success(t, f.serve(owner, http.MethodPost, "/v1/public/order/close", closeBody("no-such-order")), nil)
	if closed := f.h.ReloadOrder(orderNo); closed.Status != order.StatusClosed {
		t.Fatalf("order status = %d after the retry, want closed", closed.Status)
	}
}

// Only the buyer may abandon an order: another account's close is refused
// and the order stays payable.
func TestCloseOrderHandlerRefusesAnotherBuyersOrder(t *testing.T) {
	f := newOrderFacade(t)
	_, owner := f.buyer()
	_, stranger := f.buyer()
	orderNo := pendingOrder(t, f, owner)

	assertFailure(t, f.serve(stranger, http.MethodPost, "/v1/public/order/close", closeBody(orderNo)), xerr.InvalidAccess, "Invalid access")
	if pending := f.h.ReloadOrder(orderNo); pending.Status != order.StatusPending || len(f.stock.restored) != 0 {
		t.Fatalf("order status = %d, restored %v; want it pending with its unit", pending.Status, f.stock.restored)
	}
}

// The request is bound and validated before the facade is asked: a body that
// does not bind carries the binding error, a missing order number the
// validation message.
func TestCloseOrderHandlerRefusesInvalidRequests(t *testing.T) {
	closes := 0
	closer := closerFunc(func(context.Context, *dto.CloseOrderRequest) error {
		closes++
		return nil
	})
	for name, body := range map[string]string{
		"truncated JSON":  `{"orderNo":`,
		"wrong JSON type": `{"orderNo":5}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertFailure(t, serveClose(closer, body), xerr.InvalidParams, bindingError(t, body, &dto.CloseOrderRequest{}))
		})
	}
	for name, body := range map[string]string{
		"no order number":    `{}`,
		"empty order number": `{"orderNo":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertFailure(t, serveClose(closer, body), xerr.InvalidParams, "OrderNo is a required field")
		})
	}
	if closes != 0 {
		t.Fatalf("an invalid request reached the facade %d times", closes)
	}
}

// The facade's coded refusals pass through, an unconfirmed payment keeps its
// retryable code however the facade reported it, and an uncoded failure is
// an internal error whose detail stays in the server.
func TestCloseOrderHandlerMapsFacadeFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		err  error
		code uint32
		msg  string
	}{
		"coded refusal":               {err: xerr.Errorf(xerr.OrderNotExist, "order gone"), code: xerr.OrderNotExist, msg: "Order does not exist"},
		"unconfirmed, already mapped": {err: xerr.Wrapf(billing.ErrGatewayUnconfirmed, xerr.PaymentStatusUnconfirmed, "close order o-1"), code: xerr.PaymentStatusUnconfirmed, msg: "PAYMENT_STATUS_UNCONFIRMED"},
		"uncoded failure":             {err: errors.New("database connection reset"), code: xerr.ERROR, msg: "Internal Server Error"},
	} {
		t.Run(name, func(t *testing.T) {
			var got *dto.CloseOrderRequest
			closer := closerFunc(func(_ context.Context, req *dto.CloseOrderRequest) error {
				got = req
				return tt.err
			})
			assertFailure(t, serveClose(closer, closeBody("o-1")), tt.code, tt.msg)
			if got == nil || got.OrderNo != "o-1" {
				t.Fatalf("facade request = %+v, want order o-1", got)
			}
		})
	}
}
