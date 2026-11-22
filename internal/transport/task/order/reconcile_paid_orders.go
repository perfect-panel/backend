package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
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
	conflictRepaired                        // corrupt conflicting task discarded and replaced
)

// ReconcilePaidOrdersHandler treats the durable Paid state as an activation
// outbox. It repairs the database/Redis gap if a callback committed payment but
// Redis was unavailable before the activation task could be inserted.
type ReconcilePaidOrdersHandler struct {
	orders    orderScanner
	queue     *taskqueue.Client
	inspector *asynq.Inspector
}

// NewReconcilePaidOrdersHandler builds the reconciler over billing's order scan
// and the activation queue it repairs.
func NewReconcilePaidOrdersHandler(deps Dependencies) *ReconcilePaidOrdersHandler {
	return &ReconcilePaidOrdersHandler{orders: deps.Billing, queue: deps.Queue, inspector: deps.Inspector}
}

func (h *ReconcilePaidOrdersHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
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
		totalRepaired    int
		totalCompleted   int
		totalAggregating int
		conflictFailed   int
		totalStale       int
		oldestAge        time.Duration
	)

	for {
		orders, err := h.orders.OrdersByStatusAfter(ctx, OrderStatusPaid, afterID, paidOrderReconcileBatchSize)
		if err != nil {
			return err
		}
		for _, orderInfo := range orders {
			totalScanned++
			if isStalePaid(orderInfo.UpdatedAt, timeutil.Now()) {
				totalStale++
				if age := time.Since(orderInfo.UpdatedAt); age > oldestAge {
					oldestAge = age
				}
			}

			payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: orderInfo.OrderNo})
			if err != nil {
				return err
			}
			task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)
			taskID := taskqueue.ActivationTaskID(orderInfo.OrderNo)
			_, err = h.queue.EnqueueContext(ctx, task, asynq.MaxRetry(5), asynq.TaskID(taskID))
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
			action, state, conflictErr := h.handleConflict(ctx, orderInfo.OrderNo, taskID)
			if conflictErr != nil {
				// One order's broken task must not abort the whole run: the
				// remaining orders still get reconciled and the next cycle
				// retries this one.
				logger.WithContext(ctx).Error("[ReconcilePaidOrders] handleConflict failed",
					logger.Field("orderNo", orderInfo.OrderNo),
					logger.Field("taskID", taskID),
					logger.Field("error", conflictErr.Error()),
				)
				conflictFailed++
				afterID = orderInfo.Id
				continue
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
			case conflictRepaired:
				totalRepaired++
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
		logger.Field("repaired", totalRepaired),
		logger.Field("conflictFailed", conflictFailed),
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

func (h *ReconcilePaidOrdersHandler) handleConflict(ctx context.Context, orderNo, taskID string) (conflictAction, asynq.TaskState, error) {
	info, err := h.inspector.GetTaskInfo("default", taskID)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskNotFound) {
			if enqErr := h.enqueueActivation(ctx, orderNo, taskID); enqErr != nil {
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
		return h.handleArchived(ctx, orderNo, taskID, info)
	case asynq.TaskStateCompleted:
		// Deterministic per-task anomalies stay loud in the log but must not
		// error out: an error here aborts every remaining order's reconcile
		// and repeats each cycle.
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] CompletedWhilePaid",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("state", "completed"),
		)
		return conflictKept, asynq.TaskStateCompleted, nil
	case asynq.TaskStateAggregating:
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] UnexpectedAggregating",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("state", "aggregating"),
		)
		return conflictKept, asynq.TaskStateAggregating, nil
	default:
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] UnexpectedTaskState",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("state", info.State),
		)
		return conflictKept, info.State, nil
	}
}

// enqueueActivation inserts a fresh activation task carrying the canonical
// payload for the order.
func (h *ReconcilePaidOrdersHandler) enqueueActivation(ctx context.Context, orderNo, taskID string) error {
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: orderNo})
	if err != nil {
		return fmt.Errorf("marshal activation payload: %w", err)
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)
	if _, err = h.queue.EnqueueContext(ctx, task, asynq.MaxRetry(5), asynq.TaskID(taskID)); err != nil {
		return fmt.Errorf("enqueue activation: %w", err)
	}
	return nil
}

// discardAndReplace drops a corrupt conflicting task (wrong type, unreadable
// or mismatched payload — e.g. residue from an older deployment sharing the
// task id) and replaces it with a fresh activation task. Production showed
// that failing on these loops the whole reconcile run forever.
func (h *ReconcilePaidOrdersHandler) discardAndReplace(ctx context.Context, orderNo, taskID string) (conflictAction, asynq.TaskState, error) {
	if err := h.inspector.DeleteTask("default", taskID); err != nil {
		return conflictKept, asynq.TaskStateArchived, fmt.Errorf("delete corrupt task: %w", err)
	}
	if err := h.enqueueActivation(ctx, orderNo, taskID); err != nil {
		return conflictKept, asynq.TaskStateArchived, err
	}
	return conflictRepaired, 0, nil
}

func (h *ReconcilePaidOrdersHandler) handleArchived(ctx context.Context, orderNo, taskID string, info *asynq.TaskInfo) (conflictAction, asynq.TaskState, error) {
	if info.Type != taskqueue.ForthwithActivateOrder {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] ArchivedTypeMismatch",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("expectedType", taskqueue.ForthwithActivateOrder),
			logger.Field("actualType", info.Type),
		)
		return h.discardAndReplace(ctx, orderNo, taskID)
	}
	var payload taskqueue.ForthwithActivateOrderPayload
	if err := json.Unmarshal(info.Payload, &payload); err != nil {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] ArchivedPayloadUnmarshalFailed",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("error", err.Error()),
		)
		return h.discardAndReplace(ctx, orderNo, taskID)
	}
	if payload.OrderNo != orderNo {
		logger.WithContext(ctx).Error("[ReconcilePaidOrders] ArchivedOrderNoMismatch",
			logger.Field("orderNo", orderNo),
			logger.Field("taskID", taskID),
			logger.Field("payloadOrderNo", payload.OrderNo),
		)
		return h.discardAndReplace(ctx, orderNo, taskID)
	}

	if err := h.inspector.RunTask("default", taskID); err != nil {
		reInfo, reErr := h.inspector.GetTaskInfo("default", taskID)
		if reErr != nil {
			return conflictKept, 0, fmt.Errorf("run archived task: %w; re-read also failed: %w", err, reErr)
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
