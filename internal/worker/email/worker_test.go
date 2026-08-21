package emailworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/module/platform/entity/task"
)

type workerTaskStore struct {
	task         *task.Task
	updateErr    error
	updates      int
	rejectActive bool
}

func (s *workerTaskStore) FindOneByType(_ context.Context, _ int64, typ task.Type) (*task.Task, error) {
	if s.task == nil || s.task.Type != int8(typ) {
		return nil, errors.New("task not found")
	}
	data := *s.task
	return &data, nil
}

func (s *workerTaskStore) UpdateActive(_ context.Context, data *task.Task) (bool, error) {
	if s.updateErr != nil {
		return false, s.updateErr
	}
	if s.rejectActive {
		return false, nil
	}
	s.updates++
	s.task = data
	return true, nil
}

type workerSender struct {
	sent []string
	err  error
}

func (s *workerSender) Send(to []string, _, _ string) error {
	s.sent = append(s.sent, to...)
	return s.err
}

func newEmailTask(t *testing.T, status int8, current uint64, recipients ...string) *task.Task {
	t.Helper()
	scope, err := (&task.EmailScope{Recipients: recipients, Limit: 10}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	content, err := (&task.EmailContent{Subject: "subject", Content: "content"}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return &task.Task{Id: 7, Type: task.TypeEmail, Status: status, Current: current, Total: uint64(len(recipients)), Scope: string(scope), Content: string(content)}
}

func TestWorkerResumesFromPersistedProgress(t *testing.T) {
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 1, "a@example.com", "b@example.com")}
	sender := &workerSender{}

	if err := NewWorker(context.Background(), 7, store, sender).Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "b@example.com" {
		t.Fatalf("resumed sends = %v, want only second recipient", sender.sent)
	}
	if store.task.Status != task.StatusCompleted || store.task.Current != 2 {
		t.Fatalf("final task = %+v", store.task)
	}
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(store.task.Scope)); err != nil {
		t.Fatal(err)
	}
	if scope.DailyDate == "" || scope.DailySent != 1 {
		t.Fatalf("daily limit state was not persisted: %+v", scope)
	}
}

func TestWorkerMarksAllSendFailuresFailed(t *testing.T) {
	store := &workerTaskStore{task: newEmailTask(t, task.StatusPending, 0, "a@example.com")}
	sender := &workerSender{err: errors.New("smtp rejected")}

	if err := NewWorker(context.Background(), 7, store, sender).Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.task.Status != task.StatusFailed || store.task.Errors == "" || store.task.Current != 1 {
		t.Fatalf("failed send task = %+v", store.task)
	}
}

func TestWorkerReturnsPersistenceFailureForQueueRetry(t *testing.T) {
	store := &workerTaskStore{task: newEmailTask(t, task.StatusPending, 0, "a@example.com"), updateErr: errors.New("database unavailable")}
	if err := NewWorker(context.Background(), 7, store, &workerSender{}).Start(); err == nil {
		t.Fatal("persistence failure must be returned to the queue")
	}
}

func TestWorkerCannotOverwriteCancelledTaskWhenRecordingInvalidData(t *testing.T) {
	data := newEmailTask(t, task.StatusPending, 0, "a@example.com")
	data.Scope = `{`
	store := &workerTaskStore{task: data, rejectActive: true}

	err := NewWorker(context.Background(), 7, store, &workerSender{}).Start()
	if !errors.Is(err, ErrTaskNotActive) {
		t.Fatalf("Start error = %v, want ErrTaskNotActive", err)
	}
	if store.task.Status != task.StatusPending || store.task.Errors != "" {
		t.Fatalf("rejected stale update changed stored task: %+v", store.task)
	}
}

func TestWorkerRejectsCorruptPersistedErrors(t *testing.T) {
	data := newEmailTask(t, task.StatusPending, 0, "a@example.com")
	data.Errors = `{`
	store := &workerTaskStore{task: data}
	sender := &workerSender{}

	if err := NewWorker(context.Background(), 7, store, sender).Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.task.Status != task.StatusFailed || len(sender.sent) != 0 {
		t.Fatalf("corrupt task errors were ignored: task=%+v sent=%v", store.task, sender.sent)
	}
}

func TestWorkerReturnsDailyContinuationWithoutSending(t *testing.T) {
	data := newEmailTask(t, task.StatusInProgress, 0, "a@example.com")
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(data.Scope)); err != nil {
		t.Fatal(err)
	}
	scope.Limit = 1
	scope.DailySent = 1
	scope.DailyDate = time.Now().Format(time.DateOnly)
	encoded, _ := scope.Marshal()
	data.Scope = string(encoded)
	store := &workerTaskStore{task: data}
	sender := &workerSender{}

	err := NewWorker(context.Background(), 7, store, sender).Start()
	var continuation *DailyLimitReached
	if !errors.As(err, &continuation) || !continuation.NextAt.After(time.Now()) {
		t.Fatalf("daily continuation error = %v", err)
	}
	if len(sender.sent) != 0 || store.task.Current != 0 || store.task.Status != task.StatusInProgress {
		t.Fatalf("daily-limited worker changed delivery state: sent=%v task=%+v", sender.sent, store.task)
	}
}
