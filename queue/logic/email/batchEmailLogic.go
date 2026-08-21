package emailLogic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/hibiken/asynq"
	taskEntity "github.com/perfect-panel/server/internal/module/platform/entity/task"
	"github.com/perfect-panel/server/internal/svc"
	emailworker "github.com/perfect-panel/server/internal/worker/email"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
)

type BatchEmailLogic struct {
	svcCtx *svc.ServiceContext
}

type ErrorInfo struct {
	Error string `json:"error"`
	Email string `json:"email"`
	Time  int64  `json:"time"`
}

func NewBatchEmailLogic(svcCtx *svc.ServiceContext) *BatchEmailLogic {
	return &BatchEmailLogic{svcCtx: svcCtx}
}

func (l *BatchEmailLogic) ProcessTask(ctx context.Context, queuedTask *asynq.Task) error {
	payload := queuedTask.Payload()
	if len(payload) == 0 {
		logger.Error("[BatchEmailLogic] ProcessTask failed: empty payload")
		return asynq.SkipRetry
	}
	taskID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		logger.WithContext(ctx).Error("[BatchEmailLogic] ProcessTask failed: invalid task ID",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(payload)),
		)
		return asynq.SkipRetry
	}
	if l.svcCtx == nil || l.svcCtx.Store == nil {
		return errors.New("batch email task store is nil")
	}
	tasks := l.svcCtx.Store.Task()
	taskInfo, err := tasks.FindOneByType(ctx, taskID, taskEntity.TypeEmail)
	if err != nil {
		return l.handleFailure(ctx, taskID, err)
	}
	if taskInfo.Status == taskEntity.StatusCompleted || taskInfo.Status == taskEntity.StatusCancelled || taskInfo.Status == taskEntity.StatusEnqueueFailed ||
		(taskInfo.Status == taskEntity.StatusFailed && taskInfo.Current >= taskInfo.Total) {
		return nil
	}
	if taskInfo.Status == taskEntity.StatusFailed {
		updated, err := tasks.UpdateStatusFrom(ctx, taskID, taskEntity.TypeEmail, []int8{taskEntity.StatusFailed}, taskEntity.StatusPending)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
	}
	sender, err := email.NewSender(l.svcCtx.Config.Email.Platform, l.svcCtx.Config.Email.PlatformConfig, l.svcCtx.Config.Site.SiteName)
	if err != nil {
		logger.WithContext(ctx).Error("[BatchEmailLogic] NewSender failed", logger.Field("error", err.Error()))
		return l.handleFailure(ctx, taskID, err)
	}
	manager := emailworker.NewWorkerManager()
	if manager == nil {
		logger.WithContext(ctx).Error("[BatchEmailLogic] ProcessTask failed: worker manager is nil")
		return asynq.SkipRetry
	}

	err = manager.RunWorker(ctx, taskID, tasks, sender)
	if errors.Is(err, emailworker.ErrTaskNotActive) {
		return nil
	}
	var dailyLimit *emailworker.DailyLimitReached
	if !errors.As(err, &dailyLimit) {
		return l.handleFailure(ctx, taskID, err)
	}
	if l.svcCtx.Queue == nil {
		return errors.New("batch email continuation queue is nil")
	}
	continuation := asynq.NewTask(queuedTask.Type(), queuedTask.Payload())
	continuationID := fmt.Sprintf("marketing-email-%d-%s", taskID, dailyLimit.NextAt.Format("20060102"))
	_, enqueueErr := l.svcCtx.Queue.EnqueueContext(ctx, continuation, asynq.ProcessAt(dailyLimit.NextAt), asynq.TaskID(continuationID))
	if errors.Is(enqueueErr, asynq.ErrTaskIDConflict) {
		return nil
	}
	return l.handleFailure(ctx, taskID, enqueueErr)
}

func (l *BatchEmailLogic) handleFailure(ctx context.Context, taskID int64, cause error) error {
	if cause == nil {
		return nil
	}
	retried, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	if !retryOK || !maxOK || retried < maxRetry {
		return cause
	}
	if l.svcCtx == nil || l.svcCtx.Store == nil {
		return cause
	}
	tasks := l.svcCtx.Store.Task()
	data, err := tasks.FindOneByType(ctx, taskID, taskEntity.TypeEmail)
	if err != nil {
		return errors.Join(cause, err)
	}
	data.Status = taskEntity.StatusFailed
	var taskErrors []ErrorInfo
	if data.Errors != "" {
		if unmarshalErr := json.Unmarshal([]byte(data.Errors), &taskErrors); unmarshalErr != nil {
			taskErrors = append(taskErrors, ErrorInfo{Error: data.Errors, Time: timeutil.Now().Unix()})
		}
	}
	taskErrors = append(taskErrors, ErrorInfo{Error: cause.Error(), Time: timeutil.Now().Unix()})
	encoded, marshalErr := json.Marshal(taskErrors)
	if marshalErr != nil {
		return errors.Join(cause, marshalErr)
	}
	data.Errors = string(encoded)
	if _, err := tasks.UpdateActive(ctx, data); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}
