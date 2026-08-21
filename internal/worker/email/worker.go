package emailworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/module/platform/entity/task"
	emailpkg "github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
)

type ErrorInfo struct {
	Error string `json:"error"`
	Email string `json:"email"`
	Time  int64  `json:"time"`
}

// DailyLimitReached asks the queue shell to persist a continuation for the
// next civil day instead of keeping one queue worker asleep for hours.
type DailyLimitReached struct {
	NextAt time.Time
}

var ErrTaskNotActive = errors.New("batch email task is no longer active")

func (e *DailyLimitReached) Error() string {
	return fmt.Sprintf("batch email daily limit reached; resume at %s", e.NextAt.Format(time.RFC3339))
}

type Worker struct {
	id     int64
	tasks  TaskStore
	ctx    context.Context
	sender emailpkg.Sender
}

func NewWorker(ctx context.Context, id int64, tasks TaskStore, sender emailpkg.Sender) *Worker {
	return &Worker{id: id, tasks: tasks, ctx: ctx, sender: sender}
}

func (w *Worker) GetID() int64 { return w.id }

// Start processes a batch-email task until completion or cancellation.
func (w *Worker) Start() error {
	taskInfo, err := w.tasks.FindOneByType(w.ctx, w.id, task.TypeEmail)
	if err != nil {
		logger.Error("Batch Send Email", logger.Field("message", "Failed to find task"), logger.Field("error", err.Error()), logger.Field("task_id", w.id))
		return err
	}
	if taskInfo.Status == task.StatusCompleted || taskInfo.Status == task.StatusFailed || taskInfo.Status == task.StatusCancelled || taskInfo.Status == task.StatusEnqueueFailed {
		logger.Info("Batch Send Email", logger.Field("message", "Task is already terminal"), logger.Field("task_id", w.id), logger.Field("status", taskInfo.Status))
		return nil
	}

	var scope task.EmailScope
	if err := json.Unmarshal([]byte(taskInfo.Scope), &scope); err != nil {
		logger.Error("Batch Send Email", logger.Field("message", "Failed to parse task scope"), logger.Field("error", err.Error()), logger.Field("task_id", w.id))
		return w.failTask(taskInfo, fmt.Errorf("parse task scope: %w", err))
	}
	if len(scope.Recipients) == 0 && len(scope.Additional) == 0 {
		logger.Error("Batch Send Email", logger.Field("message", "No recipients or additional emails provided"), logger.Field("task_id", w.id))
		return w.failTask(taskInfo, fmt.Errorf("no recipients provided"))
	}

	var content task.EmailContent
	if err := json.Unmarshal([]byte(taskInfo.Content), &content); err != nil {
		logger.Error("Batch Send Email", logger.Field("message", "Failed to parse task content"), logger.Field("error", err.Error()), logger.Field("task_id", w.id))
		return w.failTask(taskInfo, fmt.Errorf("parse task content: %w", err))
	}

	recipients := tool.RemoveDuplicateElements(append(scope.Recipients, scope.Additional...)...)
	if len(recipients) == 0 {
		logger.Error("Batch Send Email", logger.Field("message", "No valid recipients found"), logger.Field("task_id", w.id))
		return w.failTask(taskInfo, fmt.Errorf("no valid recipients found"))
	}
	if taskInfo.Current > uint64(len(recipients)) {
		return w.failTask(taskInfo, fmt.Errorf("task progress exceeds recipient count"))
	}

	interval := time.Second
	if scope.Interval != 0 {
		interval = time.Duration(scope.Interval) * time.Second
	}

	var sendErrors []ErrorInfo
	if taskInfo.Errors != "" {
		if err := json.Unmarshal([]byte(taskInfo.Errors), &sendErrors); err != nil {
			return w.failTask(taskInfo, fmt.Errorf("parse task errors: %w", err))
		}
	}
	taskInfo.Status = task.StatusInProgress
	if err := w.persist(taskInfo, &scope); err != nil {
		return err
	}

	for index := int(taskInfo.Current); index < len(recipients); index++ {
		if err := w.ensureDailyCapacity(&scope, taskInfo); err != nil {
			return err
		}
		select {
		case <-w.ctx.Done():
			logger.Info("Batch Send Email", logger.Field("message", "Worker stopped by context cancellation"), logger.Field("task_id", w.id))
			return w.ctx.Err()
		default:
		}

		recipient := recipients[index]
		sendErr := w.send(recipient, content)
		if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
			return sendErr
		}
		if sendErr != nil {
			logger.Error("Batch Send Email", logger.Field("message", "Failed to send email"), logger.Field("error", sendErr.Error()), logger.Field("task_id", w.id))
			sendErrors = append(sendErrors, ErrorInfo{Error: sendErr.Error(), Email: recipient, Time: timeutil.Now().Unix()})
			text, _ := json.Marshal(sendErrors)
			taskInfo.Errors = string(text)
		}
		taskInfo.Current = uint64(index + 1)
		scope.DailySent++
		if err := w.persist(taskInfo, &scope); err != nil {
			logger.Error("Batch Send Email", logger.Field("message", "Failed to update task progress"), logger.Field("error", err.Error()), logger.Field("task_id", w.id))
			return err
		}
		if index+1 < len(recipients) {
			if err := waitContext(w.ctx, interval); err != nil {
				return err
			}
		}
	}
	taskInfo.Status = task.StatusCompleted
	failedRecipients := make(map[string]struct{}, len(sendErrors))
	for _, item := range sendErrors {
		if item.Email != "" {
			failedRecipients[item.Email] = struct{}{}
		}
	}
	if len(failedRecipients) >= len(recipients) {
		taskInfo.Status = task.StatusFailed
	}

	if err := w.persist(taskInfo, &scope); err != nil {
		logger.Error("Batch Send Email", logger.Field("message", "Failed to finalize task"), logger.Field("error", err.Error()), logger.Field("task_id", w.id))
		return err
	}
	logger.Info("Batch Send Email", logger.Field("message", "Task completed"), logger.Field("task_id", w.id), logger.Field("total_attempted", taskInfo.Current))
	return nil
}

func (w *Worker) send(recipient string, content task.EmailContent) error {
	select {
	case sendOne <- struct{}{}:
		defer func() { <-sendOne }()
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
	return w.sender.Send([]string{recipient}, content.Subject, content.Content)
}

func (w *Worker) persist(taskInfo *task.Task, scope *task.EmailScope) error {
	scopeBytes, err := scope.Marshal()
	if err != nil {
		return err
	}
	taskInfo.Scope = string(scopeBytes)
	updated, err := w.tasks.UpdateActive(w.ctx, taskInfo)
	if err != nil {
		return err
	}
	if !updated {
		return ErrTaskNotActive
	}
	return nil
}

func (w *Worker) failTask(taskInfo *task.Task, cause error) error {
	taskInfo.Status = task.StatusFailed
	taskInfo.Errors = cause.Error()
	updated, err := w.tasks.UpdateActive(w.ctx, taskInfo)
	if err != nil {
		return fmt.Errorf("record failed email task: %w", err)
	}
	if !updated {
		return ErrTaskNotActive
	}
	return nil
}

func (w *Worker) ensureDailyCapacity(scope *task.EmailScope, taskInfo *task.Task) error {
	if scope.Limit == 0 {
		return nil
	}
	now := timeutil.Now()
	today := now.Format(time.DateOnly)
	if scope.DailyDate != today {
		scope.DailyDate = today
		scope.DailySent = 0
		return w.persist(taskInfo, scope)
	}
	if scope.DailySent < scope.Limit {
		return nil
	}
	tomorrow := now.AddDate(0, 0, 1)
	nextDay := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, tomorrow.Location())
	return &DailyLimitReached{NextAt: nextDay}
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
