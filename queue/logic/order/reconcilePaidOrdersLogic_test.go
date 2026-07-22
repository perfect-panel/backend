package orderLogic

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
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

func newReconcileTestContext(t *testing.T, orders []*orderEntity.Order) (*svc.ServiceContext, *miniredis.Miniredis) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisOpt := asynq.RedisClientOpt{Addr: redisServer.Addr()}
	queue := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = queue.Close() })
	inspector := asynq.NewInspector(redisOpt)
	t.Cleanup(func() { _ = inspector.Close() })
	return &svc.ServiceContext{
		Store:     reconcileStore{orders: reconcileOrderRepo{orders: orders}},
		Queue:     queue,
		Inspector: inspector,
	}, redisServer
}

func TestReconcilePaidOrdersEnqueuesEachPaidOrderIdempotently(t *testing.T) {
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: "paid-1", Status: OrderStatusPaid},
		{Id: 2, OrderNo: "pending", Status: OrderStatusPending},
		{Id: 3, OrderNo: "paid-2", Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("duplicate ProcessTask: %v", err)
	}
	tasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected exactly 2 activation tasks, got %d", len(tasks))
	}
}

func TestReconcilePaidOrdersArchivedRecovery(t *testing.T) {
	orderNo := "archived-test-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("first ProcessTask: %v", err)
	}

	err := svcCtx.Inspector.ArchiveTask("default", taskID)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}

	archivedTasks, err := svcCtx.Inspector.ListArchivedTasks("default")
	if err != nil {
		t.Fatalf("ListArchivedTasks: %v", err)
	}
	if len(archivedTasks) != 1 {
		t.Fatalf("expected 1 archived task, got %d", len(archivedTasks))
	}

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("recovery ProcessTask: %v", err)
	}

	taskInfo, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if taskInfo.State != asynq.TaskStatePending {
		t.Fatalf("expected task to be pending after recovery, got %v", taskInfo.State)
	}
	if taskInfo.Type != types.ForthwithActivateOrder {
		t.Fatalf("expected task type %s, got %s", types.ForthwithActivateOrder, taskInfo.Type)
	}

	pendingTasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pendingTasks) != 1 {
		t.Fatalf("expected exactly 1 pending task, got %d", len(pendingTasks))
	}
}

func TestReconcilePaidOrdersNonArchivedPreserved(t *testing.T) {
	orderNo := "preserved-test-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("first ProcessTask: %v", err)
	}

	info, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if info.State != asynq.TaskStatePending {
		t.Fatalf("expected pending initially, got %v", info.State)
	}

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("second ProcessTask: %v", err)
	}

	info, err = svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if info.State != asynq.TaskStatePending {
		t.Fatalf("expected pending preserved, got %v", info.State)
	}

	pendingTasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pendingTasks) != 1 {
		t.Fatalf("expected exactly 1 pending task, got %d", len(pendingTasks))
	}
}

func TestReconcilePaidOrdersArchivedTypeMismatch(t *testing.T) {
	orderNo := "type-mismatch-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	payload, _ := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderNo})
	wrongTypeTask := asynq.NewTask("some-other-type", payload, asynq.MaxRetry(5))
	_, err := svcCtx.Queue.EnqueueContext(context.Background(), wrongTypeTask, asynq.TaskID(taskID))
	if err != nil {
		t.Fatalf("EnqueueContext with wrong type: %v", err)
	}

	err = svcCtx.Inspector.ArchiveTask("default", taskID)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}

	err = logic.ProcessTask(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ProcessTask error for type mismatch")
	}

	taskInfo, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if taskInfo.State != asynq.TaskStateArchived {
		t.Fatalf("expected task to remain archived after type mismatch, got %v", taskInfo.State)
	}
}

func TestReconcilePaidOrdersArchivedPayloadOrderNoMismatch(t *testing.T) {
	orderNo := "order-match-1"
	otherOrderNo := "order-match-other"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	payload, _ := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: otherOrderNo})
	task := asynq.NewTask(types.ForthwithActivateOrder, payload, asynq.MaxRetry(5))
	_, err := svcCtx.Queue.EnqueueContext(context.Background(), task, asynq.TaskID(taskID))
	if err != nil {
		t.Fatalf("EnqueueContext: %v", err)
	}

	err = svcCtx.Inspector.ArchiveTask("default", taskID)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}

	err = logic.ProcessTask(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ProcessTask error for OrderNo mismatch")
	}

	taskInfo, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if taskInfo.State != asynq.TaskStateArchived {
		t.Fatalf("expected task to remain archived after OrderNo mismatch, got %v", taskInfo.State)
	}
}

func TestReconcilePaidOrdersNotFoundReenqueue(t *testing.T) {
	orderNo := "notfound-1"
	svcCtx, redisSrv := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("first ProcessTask: %v", err)
	}

	err := svcCtx.Inspector.ArchiveTask("default", taskID)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}

	redisSrv.Del("asynq:{default}:t:" + taskID)
	redisSrv.ZRem("asynq:{default}:archived", taskID)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("recovery ProcessTask: %v", err)
	}

	taskInfo, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if taskInfo.State != asynq.TaskStatePending {
		t.Fatalf("expected task to be re-enqueued as pending, got %v", taskInfo.State)
	}

	pendingTasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pendingTasks) != 1 {
		t.Fatalf("expected exactly 1 pending task, got %d", len(pendingTasks))
	}
}

func TestReconcilePaidOrdersOnlyPaidAreEnqueued(t *testing.T) {
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: "paid-1", Status: OrderStatusPaid},
		{Id: 2, OrderNo: "close-1", Status: OrderStatusClose},
		{Id: 3, OrderNo: "failed-1", Status: OrderStatusFailed},
		{Id: 4, OrderNo: "finished-1", Status: OrderStatusFinished},
		{Id: 5, OrderNo: "paid-2", Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	tasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected exactly 2 activation tasks for paid orders, got %d", len(tasks))
	}
}

func TestIsStalePaid(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		updatedAt time.Time
		wantStale bool
	}{
		{name: "now", updatedAt: now, wantStale: false},
		{name: "9 minutes", updatedAt: now.Add(-9 * time.Minute), wantStale: false},
		{name: "11 minutes", updatedAt: now.Add(-11 * time.Minute), wantStale: true},
		{name: "20 minutes", updatedAt: now.Add(-20 * time.Minute), wantStale: true},
		{name: "1 hour", updatedAt: now.Add(-1 * time.Hour), wantStale: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStalePaid(tt.updatedAt, now)
			if got != tt.wantStale {
				t.Errorf("isStalePaid(%v, %v) = %v, want %v", tt.updatedAt, now, got, tt.wantStale)
			}
		})
	}
}

func TestReconcilePaidOrdersStaleDetection(t *testing.T) {
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: "fresh", Status: OrderStatusPaid, UpdatedAt: time.Now()},
		{Id: 2, OrderNo: "truly-stale", Status: OrderStatusPaid, UpdatedAt: time.Now().Add(-20 * time.Minute)},
		{Id: 3, OrderNo: "recently-paid-old-creation", Status: OrderStatusPaid, CreatedAt: time.Now().Add(-20 * time.Minute), UpdatedAt: time.Now()},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	tasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 pending tasks, got %d", len(tasks))
	}
}

func TestReconcilePaidOrdersArchivedRunTaskRace(t *testing.T) {
	orderNo := "race-test-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("first ProcessTask: %v", err)
	}

	var err error
	err = svcCtx.Inspector.ArchiveTask("default", taskID)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}

	err = svcCtx.Inspector.RunTask("default", taskID)
	if err != nil {
		t.Fatalf("RunTask (simulating concurrent recovery before reconcile): %v", err)
	}
	taskInfo, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if taskInfo.State != asynq.TaskStatePending {
		t.Fatalf("expected pending after RunTask, got %v", taskInfo.State)
	}

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("recovery ProcessTask: %v", err)
	}

	taskInfo, err = svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if taskInfo.State != asynq.TaskStatePending {
		t.Fatalf("expected task to remain pending after race scenario, got %v", taskInfo.State)
	}

	pendingTasks, err := svcCtx.Inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pendingTasks) != 1 {
		t.Fatalf("expected exactly 1 pending task, got %d", len(pendingTasks))
	}
}

func TestReconcilePaidOrdersHandleArchivedBenignRace(t *testing.T) {
	orderNo := "handle-archived-race-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	payload, _ := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderNo})
	task := asynq.NewTask(types.ForthwithActivateOrder, payload, asynq.MaxRetry(5))
	_, err := svcCtx.Queue.EnqueueContext(context.Background(), task, asynq.TaskID(taskID))
	if err != nil {
		t.Fatalf("EnqueueContext: %v", err)
	}

	info, err := svcCtx.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if info.State != asynq.TaskStatePending {
		t.Fatalf("expected pending, got %v", info.State)
	}

	fakeInfo := &asynq.TaskInfo{
		ID:      taskID,
		Queue:   "default",
		Type:    types.ForthwithActivateOrder,
		Payload: payload,
		State:   asynq.TaskStateArchived,
	}

	action, state, err := logic.handleArchived(context.Background(), orderNo, taskID, fakeInfo)
	if err != nil {
		t.Fatalf("handleArchived: %v", err)
	}
	if action != conflictKept {
		t.Fatalf("expected conflictKept, got %v", action)
	}
	if state != asynq.TaskStatePending {
		t.Fatalf("expected pending state after benign race, got %v", state)
	}
}

func TestReconcilePaidOrdersHandleArchivedTypeMismatchReturnsError(t *testing.T) {
	orderNo := "handle-archived-type-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	payload, _ := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderNo})
	fakeInfo := &asynq.TaskInfo{
		ID:      taskID,
		Queue:   "default",
		Type:    "some-other-type",
		Payload: payload,
		State:   asynq.TaskStateArchived,
	}

	_, _, err := logic.handleArchived(context.Background(), orderNo, taskID, fakeInfo)
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
}

func TestReconcilePaidOrdersHandleArchivedOrderNoMismatchReturnsError(t *testing.T) {
	orderNo := "handle-archived-oid-1"
	svcCtx, _ := newReconcileTestContext(t, []*orderEntity.Order{
		{Id: 1, OrderNo: orderNo, Status: OrderStatusPaid},
	})
	logic := NewReconcilePaidOrdersLogic(svcCtx)
	taskID := types.ActivationTaskID(orderNo)

	wrongPayload, _ := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: "some-other-order"})
	fakeInfo := &asynq.TaskInfo{
		ID:      taskID,
		Queue:   "default",
		Type:    types.ForthwithActivateOrder,
		Payload: wrongPayload,
		State:   asynq.TaskStateArchived,
	}

	_, _, err := logic.handleArchived(context.Background(), orderNo, taskID, fakeInfo)
	if err == nil {
		t.Fatal("expected error for OrderNo mismatch")
	}
}

func TestReconcilePaidOrdersMultipleBatches(t *testing.T) {
	n := 3 * paidOrderReconcileBatchSize / 2
	orders := make([]*orderEntity.Order, 0, n)
	for i := int64(1); i <= int64(n); i++ {
		orders = append(orders, &orderEntity.Order{
			Id:      i,
			OrderNo: fmt.Sprintf("batch-paid-%d", i),
			Status:  OrderStatusPaid,
		})
	}
	svcCtx, _ := newReconcileTestContext(t, orders)
	logic := NewReconcilePaidOrdersLogic(svcCtx)

	if err := logic.ProcessTask(context.Background(), nil); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	tasks, err := svcCtx.Inspector.ListPendingTasks("default", asynq.PageSize(n+1))
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(tasks) != n {
		t.Fatalf("expected %d pending tasks, got %d", n, len(tasks))
	}
}
