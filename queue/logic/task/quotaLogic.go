package task

import (
	"context"
	"errors"
	"strconv"

	"github.com/hibiken/asynq"
	taskEntity "github.com/perfect-panel/server/internal/module/platform/entity/task"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

// QuotaTaskLogic is the queue shell for the admin-scheduled quota grants;
// the business logic lives in the subscription module (ADR-001 step 6
// preparation).
type QuotaTaskLogic struct {
	svcCtx *svc.ServiceContext
}

func NewQuotaTaskLogic(svcCtx *svc.ServiceContext) *QuotaTaskLogic {
	return &QuotaTaskLogic{svcCtx: svcCtx}
}

func (l *QuotaTaskLogic) ProcessTask(ctx context.Context, t *asynq.Task) error {
	taskID, err := l.parseTaskID(ctx, t.Payload())
	if err != nil {
		return err
	}
	if err := l.svcCtx.Subscription.ProcessQuotaTask(ctx, taskID); err != nil {
		if errors.Is(err, subscription.ErrQuotaTaskUnretryable) {
			return asynq.SkipRetry
		}
		if retried, ok := asynq.GetRetryCount(ctx); ok {
			if maxRetry, maxOK := asynq.GetMaxRetry(ctx); maxOK && retried >= maxRetry {
				l.markFailed(ctx, taskID, err)
			}
		}
		return err
	}
	return nil
}

func (l *QuotaTaskLogic) markFailed(ctx context.Context, taskID int64, cause error) {
	if l.svcCtx == nil || l.svcCtx.Store == nil {
		return
	}
	tasks := l.svcCtx.Store.Task()
	data, err := tasks.FindOneByType(ctx, taskID, taskEntity.TypeQuota)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic] failed to load exhausted task", logger.Field("error", err.Error()), logger.Field("taskID", taskID))
		return
	}
	data.Status = taskEntity.StatusFailed
	data.Errors = cause.Error()
	if _, err := tasks.UpdateActive(ctx, data); err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic] failed to mark exhausted task", logger.Field("error", err.Error()), logger.Field("taskID", taskID))
	}
}

func (l *QuotaTaskLogic) parseTaskID(ctx context.Context, payload []byte) (int64, error) {
	if len(payload) == 0 {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.parseTaskID] empty payload")
		return 0, asynq.SkipRetry
	}

	taskID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.parseTaskID] invalid task ID",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(payload)),
		)
		return 0, asynq.SkipRetry
	}
	return taskID, nil
}
