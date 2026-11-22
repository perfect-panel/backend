package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/mail"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	taskEntity "github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// BatchEmailHandler runs a marketing email campaign, the batch task an
// administrator created. The task row in the platform kernel's bookkeeping
// carries the campaign and its progress, so a campaign that reaches the
// daily sending limit continues from a follow-up task the next day, one that
// runs out of its run budget continues from a follow-up task at once, and a
// retried delivery resumes where the last run stopped.
type BatchEmailHandler struct {
	deps Dependencies
	// senders keeps the provider client between campaigns; it is rebuilt
	// when the email configuration changes.
	senders mail.Senders
	// newSender builds the provider client of a run; tests replace it.
	newSender func(platform, config, siteName string) (mail.Sender, error)
}

// NewBatchEmailHandler builds the handler over the task bookkeeping, the
// message log, the queue and the runtime email settings.
func NewBatchEmailHandler(deps Dependencies) *BatchEmailHandler {
	h := &BatchEmailHandler{deps: deps}
	h.newSender = h.senders.Get
	return h
}

func (h *BatchEmailHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	payload := task.Payload()
	if len(payload) == 0 {
		logger.WithContext(ctx).Error("[BatchEmail] ProcessTask failed: empty payload")
		return asynq.SkipRetry
	}
	taskID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		logger.WithContext(ctx).Error("[BatchEmail] ProcessTask failed: invalid task ID",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(payload)),
		)
		return asynq.SkipRetry
	}
	if h.deps.Tasks == nil {
		return errors.New("batch email task store is nil")
	}
	taskInfo, err := h.deps.Tasks.FindOneByType(ctx, taskID, taskEntity.TypeEmail)
	if err != nil {
		return h.handleFailure(ctx, taskID, err)
	}
	if terminalCampaign(taskInfo) {
		return nil
	}
	if taskInfo.Status == taskEntity.StatusFailed {
		updated, err := h.deps.Tasks.UpdateStatusFrom(ctx, taskID, taskEntity.TypeEmail, []int8{taskEntity.StatusFailed}, taskEntity.StatusPending)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
	}
	if h.deps.Email == nil || h.deps.SiteName == nil {
		return h.handleFailure(ctx, taskID, errors.New("batch email runtime configuration is unavailable"))
	}
	sender, err := h.newSender(h.deps.Email().Platform, h.deps.Email().PlatformConfig, h.deps.SiteName())
	if err != nil {
		logger.WithContext(ctx).Error("[BatchEmail] NewSender failed", logger.Field("error", err.Error()))
		return h.handleFailure(ctx, taskID, err)
	}
	manager := NewWorkerManager()
	if manager == nil {
		logger.WithContext(ctx).Error("[BatchEmail] ProcessTask failed: worker manager is nil")
		return asynq.SkipRetry
	}

	err = manager.RunWorker(ctx, taskID, h.deps.Tasks, sender,
		WithMessageLogs(h.deps.Logs, h.deps.Email().Platform),
		WithRecipientResolver(h.deps.Recipients))
	var (
		dailyLimit *DailyLimitReached
		budget     *RunBudgetExhausted
	)
	switch {
	case err == nil, errors.Is(err, ErrTaskNotActive):
		return nil
	case errors.As(err, &dailyLimit):
		return h.continueFrom(ctx, task, taskID, dailyLimit.NextAt, fmt.Sprintf("marketing-email-%d-%s", taskID, dailyLimit.NextAt.Format("20060102")))
	case errors.As(err, &budget):
		// The chunk id is the resume position: a duplicate delivery of this
		// run finds the continuation already queued.
		return h.continueFrom(ctx, task, taskID, budget.ResumeAt, fmt.Sprintf("marketing-email-%d-chunk-%d", taskID, budget.Sent))
	case isInterruption(err):
		return h.resumeInterrupted(ctx, task, taskID, err)
	}
	return h.handleFailure(ctx, taskID, err)
}

// terminalCampaign reports whether nothing is left to run for the task.
func terminalCampaign(taskInfo *taskEntity.Task) bool {
	switch taskInfo.Status {
	case taskEntity.StatusCompleted, taskEntity.StatusCancelled, taskEntity.StatusEnqueueFailed:
		return true
	case taskEntity.StatusFailed:
		return taskInfo.Current >= taskInfo.Total
	}
	return false
}

// isInterruption reports whether the run was stopped rather than failed:
// its context ended, because the run's deadline passed or an administrator
// stopped the campaign. The campaign keeps its recorded progress and is never
// marked failed for it.
func isInterruption(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// continueFrom queues the follow-up task the campaign continues from at
// processAt, under continuationID. A continuation already queued under that
// id is this one.
func (h *BatchEmailHandler) continueFrom(ctx context.Context, task *asynq.Task, taskID int64, processAt time.Time, continuationID string) error {
	if h.deps.Queue == nil {
		return errors.New("batch email continuation queue is nil")
	}
	continuation := asynq.NewTask(task.Type(), task.Payload())
	_, enqueueErr := h.deps.Queue.EnqueueContext(ctx, continuation,
		asynq.ProcessAt(processAt), asynq.TaskID(continuationID), asynq.Timeout(taskqueue.BatchEmailTaskTimeout))
	if enqueueErr == nil || errors.Is(enqueueErr, asynq.ErrTaskIDConflict) {
		return nil
	}
	return h.handleFailure(ctx, taskID, enqueueErr)
}

// resumeInterrupted handles a run whose context ended. When the run's own
// deadline passed, the queue already counts the run as failed and retries
// the task, which resumes from the recorded progress: cause is returned as
// it is. Otherwise the worker was stopped from inside the process; a
// campaign an administrator cancelled is done, and one still active
// continues from a follow-up task at once.
func (h *BatchEmailHandler) resumeInterrupted(ctx context.Context, task *asynq.Task, taskID int64, cause error) error {
	if ctx.Err() != nil {
		return cause
	}
	taskInfo, err := h.deps.Tasks.FindOneByType(ctx, taskID, taskEntity.TypeEmail)
	if err != nil {
		return errors.Join(cause, err)
	}
	if terminalCampaign(taskInfo) {
		return nil
	}
	return h.continueFrom(ctx, task, taskID, timeutil.Now(), fmt.Sprintf("marketing-email-%d-chunk-%d", taskID, taskInfo.Current))
}

// handleFailure returns cause for asynq to retry the campaign. On the last
// attempt it also marks the task failed and appends cause to the task's
// recorded errors, so the administrator sees why the campaign stopped. An
// interrupted run is not a failure of the campaign: it keeps its progress and
// its status, whatever the attempt.
func (h *BatchEmailHandler) handleFailure(ctx context.Context, taskID int64, cause error) error {
	if cause == nil {
		return nil
	}
	if isInterruption(cause) {
		return cause
	}
	retried, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	if !retryOK || !maxOK || retried < maxRetry {
		return cause
	}
	if h.deps.Tasks == nil {
		return cause
	}
	data, err := h.deps.Tasks.FindOneByType(ctx, taskID, taskEntity.TypeEmail)
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
	if _, err := h.deps.Tasks.UpdateActive(ctx, data); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}
