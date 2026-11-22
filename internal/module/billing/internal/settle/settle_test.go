package settle_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
)

func TestValidateTradeNo(t *testing.T) {
	for _, bad := range []string{"", " trade", "trade ", "tr\nade", "tr\x7fade", string([]byte{0xff, 0xfe}), strings.Repeat("t", 256)} {
		if err := settle.ValidateTradeNo(bad); err == nil {
			t.Fatalf("trade number %q was accepted", bad)
		}
	}
	for _, good := range []string{"2026080222001430000000000001", "pi_3Nx", "交易-1", strings.Repeat("t", 255)} {
		if err := settle.ValidateTradeNo(good); err != nil {
			t.Fatalf("trade number %q was rejected: %v", good, err)
		}
	}
}

func setup(t *testing.T, status uint8, tradeNo string) (*billingtest.Harness, *billingtest.Queue, *order.Order) {
	t.Helper()
	h := billingtest.New(t)
	o := h.Order(&order.Order{OrderNo: "order-1", Status: status, TradeNo: tradeNo})
	return h, &billingtest.Queue{}, o
}

func TestVerifiedPaymentMarksAPendingOrderPaidOnce(t *testing.T) {
	h, queue, o := setup(t, order.StatusPending, "")
	if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, o, "trade-1"); err != nil {
		t.Fatalf("VerifiedPayment: %v", err)
	}
	paid := h.ReloadOrder(o.OrderNo)
	if paid.Status != order.StatusPaid || paid.TradeNo != "trade-1" || !slices.Equal(queue.Activations, []string{"order-1"}) {
		t.Fatalf("order = %+v activations = %v", paid, queue.Activations)
	}
	// A retried callback heals a lost activation without another transition.
	if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, paid, "trade-1"); err != nil {
		t.Fatalf("retried VerifiedPayment: %v", err)
	}
	// The seeded order has no creation event: the one event is the payment.
	if len(queue.Activations) != 2 || len(h.Events(o.OrderNo)) != 1 {
		t.Fatalf("activations = %v events = %d, want a re-enqueue and no new transition", queue.Activations, len(h.Events(o.OrderNo)))
	}
}

// A callback that read the order as pending may lose to one that settled it.
func TestVerifiedPaymentAcceptsTheSamePaymentSettledConcurrently(t *testing.T) {
	h, queue, o := setup(t, order.StatusPending, "")
	stale := *o
	if _, err := h.Store.Order().MarkOrderPaid(context.Background(), o.OrderNo, "trade-1"); err != nil {
		t.Fatal(err)
	}
	if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, &stale, "trade-1"); err != nil {
		t.Fatalf("VerifiedPayment: %v", err)
	}
	if len(queue.Activations) != 1 {
		t.Fatalf("activations = %v, want the activation enqueued", queue.Activations)
	}
	if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, &stale, "trade-2"); err == nil {
		t.Fatal("another payment of the settled order was accepted")
	}
}

func TestVerifiedPaymentLeavesAFinishedOrderAlone(t *testing.T) {
	h, queue, o := setup(t, order.StatusFinished, "trade-1")
	if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, o, "trade-1"); err != nil || len(queue.Activations) != 0 {
		t.Fatalf("VerifiedPayment = %v with activations %v, want nothing to do", err, queue.Activations)
	}
}

func TestVerifiedPaymentRejectsWhatCannotSettle(t *testing.T) {
	for name, tt := range map[string]struct {
		status  uint8
		stored  string
		tradeNo string
	}{
		"closed order":         {order.StatusClosed, "", "trade-1"},
		"failed order":         {order.StatusFailed, "", "trade-1"},
		"another payment":      {order.StatusPaid, "trade-1", "trade-2"},
		"malformed trade no":   {order.StatusPending, "", " trade"},
		"another intent bound": {order.StatusPending, "pi_1", "pi_2"},
	} {
		t.Run(name, func(t *testing.T) {
			h, queue, o := setup(t, tt.status, tt.stored)
			if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, o, tt.tradeNo); err == nil {
				t.Fatal("the payment was accepted")
			}
			if len(queue.Activations) != 0 || h.ReloadOrder(o.OrderNo).Status != tt.status {
				t.Fatal("a refused payment changed the order or enqueued its activation")
			}
		})
	}
}

// The committed Paid state is the durable outbox: an enqueue failure is
// reported for a retry and the paid-order reconciler repairs it.
func TestVerifiedPaymentReportsAnEnqueueFailureAfterTheCommit(t *testing.T) {
	h, queue, o := setup(t, order.StatusPending, "")
	queue.ActivationErr = errors.New("queue unavailable")
	if err := settle.VerifiedPayment(context.Background(), h.Store.Order(), queue, o, "trade-1"); err == nil {
		t.Fatal("the enqueue failure was hidden")
	}
	if h.ReloadOrder(o.OrderNo).Status != order.StatusPaid {
		t.Fatal("the verified payment was not committed")
	}
}
