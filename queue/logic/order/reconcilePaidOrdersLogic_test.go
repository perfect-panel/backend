package orderLogic

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

type reconcileOrderRepo struct {
	repository.OrderRepo
	orders []*orderEntity.Order
}

func (r reconcileOrderRepo) QueryOrdersByStatusAfterID(_ context.Context, status uint8, afterID int64, limit int) ([]*orderEntity.Order, error) {
	result := make([]*orderEntity.Order, 0, limit)
	for _, item := range r.orders {
		if item.Status == status && item.Id > afterID {
			result = append(result, item)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

type reconcileStore struct {
	repository.Store
	orders repository.OrderRepo
}

func (s reconcileStore) Order() repository.OrderRepo { return s.orders }

func TestReconcilePaidOrdersEnqueuesEachPaidOrderIdempotently(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisOpt := asynq.RedisClientOpt{Addr: redisServer.Addr()}
	queue := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = queue.Close() })
	inspector := asynq.NewInspector(redisOpt)
	t.Cleanup(func() { _ = inspector.Close() })

	logic := NewReconcilePaidOrdersLogic(&svc.ServiceContext{
		Store: reconcileStore{orders: reconcileOrderRepo{orders: []*orderEntity.Order{
			{Id: 1, OrderNo: "paid-1", Status: OrderStatusPaid},
			{Id: 2, OrderNo: "pending", Status: OrderStatusPending},
			{Id: 3, OrderNo: "paid-2", Status: OrderStatusPaid},
		}}},
		Queue: queue,
	})

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("duplicate ProcessTask: %v", err)
	}
	tasks, err := inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected exactly 2 activation tasks, got %d", len(tasks))
	}
}
