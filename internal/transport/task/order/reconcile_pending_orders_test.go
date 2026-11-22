package order

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	orderEntity "github.com/perfect-panel/backend/internal/module/billing/entity/order"
)

// closingOrders serves billing's order scan and records the closes it is
// asked for, failing those of the orders in failures.
type closingOrders struct {
	reconcileOrders
	closed   []string
	failures map[string]error
}

var _ pendingOrderCloser = (*closingOrders)(nil)

func (o *closingOrders) CloseOrder(_ context.Context, req *dto.CloseOrderRequest) error {
	o.closed = append(o.closed, req.OrderNo)
	return o.failures[req.OrderNo]
}

// Pending orders past the payment window are closed and younger ones wait.
// Neither a gateway that cannot confirm the payment nor a failed close stops
// the scan: those orders stay pending for the next run.
func TestReconcilePendingOrdersClosesExpiredOrders(t *testing.T) {
	expired := time.Now().Add(-pendingOrderExpiry - time.Minute)
	orders := &closingOrders{
		reconcileOrders: reconcileOrders{
			{Id: 1, OrderNo: "expired", Status: OrderStatusPending, CreatedAt: expired},
			{Id: 2, OrderNo: "young", Status: OrderStatusPending, CreatedAt: time.Now()},
			{Id: 3, OrderNo: "unconfirmed", Status: OrderStatusPending, CreatedAt: expired},
			{Id: 4, OrderNo: "failing", Status: OrderStatusPending, CreatedAt: expired},
			{Id: 5, OrderNo: "paid", Status: OrderStatusPaid, CreatedAt: expired},
			{Id: 6, OrderNo: "expired-last", Status: OrderStatusPending, CreatedAt: expired},
		},
		failures: map[string]error{"unconfirmed": billing.ErrGatewayUnconfirmed, "failing": errors.New("gateway unavailable")},
	}

	if err := (&ReconcilePendingOrdersHandler{orders: orders}).ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if want := []string{"expired", "unconfirmed", "failing", "expired-last"}; !slices.Equal(orders.closed, want) {
		t.Fatalf("closed %v, want %v", orders.closed, want)
	}
}

func TestReconcilePendingOrdersMultipleBatches(t *testing.T) {
	n := 3 * pendingOrderReconcileBatchSize / 2
	expired := time.Now().Add(-pendingOrderExpiry - time.Minute)
	orders := &closingOrders{reconcileOrders: make(reconcileOrders, 0, n)}
	for i := int64(1); i <= int64(n); i++ {
		orders.reconcileOrders = append(orders.reconcileOrders, &orderEntity.Order{
			Id: i, OrderNo: fmt.Sprintf("batch-pending-%d", i), Status: OrderStatusPending, CreatedAt: expired,
		})
	}

	if err := (&ReconcilePendingOrdersHandler{orders: orders}).ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if len(orders.closed) != n {
		t.Fatalf("closed %d orders, want all %d across the batches", len(orders.closed), n)
	}
}
