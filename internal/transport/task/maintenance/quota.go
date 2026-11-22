package maintenance

import (
	"context"
	"errors"
	"strconv"

	"github.com/pars-aria-labs/asynq"
	taskEntity "github.com/perfect-panel/server/internal/module/platform/entity/task"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
)

// QuotaTaskHandler is the queue shell of the quota grants administrators
// schedule; the grants themselves are the subscription module's. The last
// failed attempt marks the task failed in the platform kernel's task
// bookkeeping, so the administrator sees why it stopped.
type QuotaTaskHandler struct {
	service subscription.Service
	tasks   repository.TaskRepo
}

// NewQuotaTaskHandler builds the shell over the subscription facade and the
// task bookkeeping.
func NewQuotaTaskHandler(service subscription.Service, tasks repository.TaskRepo) *QuotaTaskHandler {
	return &QuotaTaskHandler{service: service, tasks: tasks}
}

func (h *QuotaTaskHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	taskID, err := h.parseTaskID(ctx, t.Payload())
	if err != nil {
		return err
	}
	if err := h.service.ProcessQuotaTask(ctx, taskID); err != nil {
		if errors.Is(err, subscription.ErrQuotaTaskUnretryable) {
			return asynq.SkipRetry
		}
		if retried, ok := asynq.GetRetryCount(ctx); ok {
			if maxRetry, maxOK := asynq.GetMaxRetry(ctx); maxOK && retried >= maxRetry {
				h.markFailed(ctx, taskID, err)
			}
		}
		return err
	}
	return nil
}

// markFailed records the final failure on the task; a bookkeeping failure is
// only logged, because the task has no retries left to report it with.
func (h *QuotaTaskHandler) markFailed(ctx context.Context, taskID int64, cause error) {
	if h.tasks == nil {
		return
	}
	data, err := h.tasks.FindOneByType(ctx, taskID, taskEntity.TypeQuota)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTask] failed to load exhausted task", logger.Field("error", err.Error()), logger.Field("taskID", taskID))
		return
	}
	data.Status = taskEntity.StatusFailed
	data.Errors = cause.Error()
	if _, err := h.tasks.UpdateActive(ctx, data); err != nil {
		logger.WithContext(ctx).Error("[QuotaTask] failed to mark exhausted task", logger.Field("error", err.Error()), logger.Field("taskID", taskID))
	}
}

// parseTaskID reads the task id the payload carries; a payload without one can
// never succeed, so it skips the retries.
func (h *QuotaTaskHandler) parseTaskID(ctx context.Context, payload []byte) (int64, error) {
	if len(payload) == 0 {
		logger.WithContext(ctx).Error("[QuotaTask] empty payload")
		return 0, asynq.SkipRetry
	}

	taskID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTask] invalid task ID",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(payload)),
		)
		return 0, asynq.SkipRetry
	}
	return taskID, nil
}
