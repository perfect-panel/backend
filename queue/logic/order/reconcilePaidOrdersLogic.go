package orderLogic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/queue/types"
)

const (
	paidOrderReconcileBatchSize = 500
	stalePaidThreshold          = 10 * time.Minute
)

func isStalePaid(updatedAt, now time.Time) bool {
	return now.Sub(updatedAt) > stalePaidThreshold
}

type conflictAction int

const (
	conflictKept      conflictAction = iota // task exists in acceptable state
	conflictNotFound                        // task vanished, re-enqueued
	conflictRecovered                       // archived task recovered via RunTask
)

// ReconcilePaidOrdersLogic treats the durable Paid state as an activation
// outbox. It repairs the database/Redis gap if a callback committed payment but
// Redis was unavailable before the activation task could be inserted.
type ReconcilePaidOrdersLogic struct {
	svc *svc.ServiceContext
}

func NewReconcilePaidOrdersLogic(svc *svc.ServiceContext) *ReconcilePaidOrdersLogic {
	return &ReconcilePaidOrdersLogic{svc: svc}
}

func (l *ReconcilePaidOrdersLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	var (
		afterID          int64
		totalScanned     int
		totalEnqueued    int
		totalConflict    int
		keptPending      int
		keptActive       int
		keptScheduled    int
		keptRetry        int
		totalNotFound    int
		totalArchived    int
		totalCompleted   int
		totalAggregating int
		totalStale       int
		oldestAge        time.Duration
	)

	for {
		orders, err := l.svc.Store.Order().QueryOrdersByStatusAfterID(ctx, OrderStatusPaid, afterID, paidOrderReconcileBatchSize)
		if err != nil {
			return err
		}
		for _, orderInfo := range orders {
			totalScanned++
			if isStalePaid(orderInfo.UpdatedAt, time.Now()) {
				totalStale++
				if age := time.Since(orderInfo.UpdatedAt); age > oldestAge {
					oldestAge = age
				}
			}

			payload, err := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderInfo.OrderNo})
			if err != nil {
				return err
			}
			task := asynq.NewTask(types.ForthwithActivateOrder, payload, asynq.MaxRetry(5))
			taskID := types.ActivationTaskID(orderInfo.OrderNo)
			_, err = l.svc.Queue.EnqueueContext(ctx, task, asynq.TaskID(taskID))
			if err == nil {
				totalEnqueued++
				afterID = orderInfo.Id
				continue
			}
			if !errors.Is(err, asynq.ErrTaskIDConflict) {
				logger.WithContext(ctx).Error("[ReconcilePaidOrders] Enqueue failed",
					logger.Field("orderNo", orderInfo.OrderNo),
					logger.Field("error", err.Error()),
				)
				return err
			}
			totalConflict++
			action, state, conflictErr := l.handleConflict(ctx, orderInfo.OrderNo, taskID)
			if conflictErr != nil {
				logger.WithContext(ctx).Error("[ReconcilePaidOrders] handleConflict failed",
					logger.Field("orderNo", orderInfo.OrderNo),
					logger.Field("taskID", taskID),
					logger.Field("error", conflictErr.Error()),
				)
				return conflictErr
			}
			switch action {
			case conflictKept:
				switch state {
				case asynq.TaskStatePending:
					keptPending++
				case asynq.TaskStateActive:
					keptActive++
				case asynq.TaskStateScheduled:
					keptScheduled++
				case asynq.TaskStateRetry:
					keptRetry++
				case asynq.TaskStateCompleted:
					totalCompleted++
				case asynq.TaskStateAggregating:
					totalAggregating++
				}
			case conflictNotFound:
				totalNotFound++
			case conflictRecovered:
				totalArchived++
			}
			afterID = orderInfo.Id
		}
		if len(orders) < paidOrderReconcileBatchSize {
			break
		}
	}

	logger.WithContext(ctx).Info("[ReconcilePaidOrders] Summary",
		logger.Field("scanned", totalScanned),
		logger.Field("enqueued", totalEnqueued),
		logger.Field("conflict", totalConflict),
		logger.Field("conflictPending", keptPending),
		logger.Field("conflictActive", keptActive),
		logger.Field("conflictScheduled", keptScheduled),
		logger.Field("conflictRetry", keptRetry),
		logger.Field("notFound", totalNotFound),
		logger.Field("archivedRecovered", totalArchived),
		logger.Field("completedWhilePaid", totalCompleted),
		logger.Field("aggregating", totalAggregating),
		logger.Field("stalePaid", totalStale),
		logger.Field("oldestAge", oldestAge),
	)

	if totalStale > 0 {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] StalePaidAlert",
			logger.Field("count", totalStale),
			logger.Field("oldestAge", oldestAge),
		)
	}
	return nil
}

func (l *ReconcilePaidOrdersLogic) handleConflict(ctx context.Context, orderNo, taskID string) (conflictAction, asynq.TaskState, error) {
	info, err := l.svc.Inspector.GetTaskInfo("default", taskID)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskNotFound) {
			payload, enqErr := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderNo})
			if enqErr != nil {
				return conflictKept, 0, fmt.Errorf("marshal for re-enqueue: %w", enqErr)
			}
			task := asynq.NewTask(types.ForthwithActivateOrder, payload, asynq.MaxRetry(5))
			if _, enqErr = l.svc.Queue.EnqueueContext(ctx, task, asynq.TaskID(taskID)); enqErr != nil {
				return conflictKept, 0, fmt.Errorf("re-enqueue after not found: %w", enqErr)
			}
			return conflictNotFound, 0, nil
		}
		return conflictKept, 0, fmt.Errorf("get task info: %w", err)
	}

	switch info.State {
	case asynq.TaskStatePending, asynq.TaskStateActive, asynq.TaskStateScheduled, asynq.TaskStateRetry:
		return conflictKept, info.State, nil
	case asynq.TaskStateArchived:
		return l.handleArchived(ctx, orderNo, taskID, info)
	case asynq.TaskStateCompleted:
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] CompletedWhilePaid",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("state", "completed"),
		)
		return conflictKept, asynq.TaskStateCompleted, fmt.Errorf("task completed while order still paid")
	case asynq.TaskStateAggregating:
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] UnexpectedAggregating",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("state", "aggregating"),
		)
		return conflictKept, asynq.TaskStateAggregating, fmt.Errorf("task aggregating while order still paid")
	default:
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] UnexpectedTaskState",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("state", info.State),
		)
		return conflictKept, info.State, fmt.Errorf("unexpected task state: %v", info.State)
	}
}

func (l *ReconcilePaidOrdersLogic) handleArchived(ctx context.Context, orderNo, taskID string, info *asynq.TaskInfo) (conflictAction, asynq.TaskState, error) {
	if info.Type != types.ForthwithActivateOrder {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] ArchivedTypeMismatch",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("expectedType", types.ForthwithActivateOrder),
			logger.Field("actualType", info.Type),
		)
		return conflictKept, asynq.TaskStateArchived, fmt.Errorf("archived type mismatch: expected %s, got %s", types.ForthwithActivateOrder, info.Type)
	}
	var payload types.ForthwithActivateOrderPayload
	if err := json.Unmarshal(info.Payload, &payload); err != nil {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] ArchivedPayloadUnmarshalFailed",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("error", err.Error()),
		)
		return conflictKept, asynq.TaskStateArchived, fmt.Errorf("archived payload unmarshal: %w", err)
	}
	if payload.OrderNo != orderNo {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] ArchivedOrderNoMismatch",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("payloadOrderNo", payload.OrderNo),
		)
		return conflictKept, asynq.TaskStateArchived, fmt.Errorf("archived order_no mismatch: expected %s, got %s", orderNo, payload.OrderNo)
	}

	if err := l.svc.Inspector.RunTask("default", taskID); err != nil {
		reInfo, reErr := l.svc.Inspector.GetTaskInfo("default", taskID)
		if reErr != nil {
			return conflictKept, 0, fmt.Errorf("run archived task: %w; re-read also failed: %v", err, reErr)
		}
		switch reInfo.State {
		case asynq.TaskStatePending, asynq.TaskStateActive, asynq.TaskStateScheduled, asynq.TaskStateRetry:
			return conflictKept, reInfo.State, nil
		default:
			return conflictKept, 0, fmt.Errorf("run archived task: %w", err)
		}
	}
	logger.WithContext(ctx).Info("[ReconcilePaidOrders] RecoveredArchived",
		logger.Field("orderNo", orderNo),
		logger.Field("taskID", taskID),
	)
	return conflictRecovered, 0, nil
}
