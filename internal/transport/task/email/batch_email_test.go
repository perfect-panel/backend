package email

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/mail"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// The worker store is the whole task repository, so the queue handler runs
// over it too.
var _ repository.TaskRepo = (*workerTaskStore)(nil)

func (s *workerTaskStore) Insert(_ context.Context, data *task.Task) error {
	s.task = data
	return nil
}

func (s *workerTaskStore) FindOne(ctx context.Context, id int64) (*task.Task, error) {
	if s.task == nil {
		return nil, errors.New("task not found")
	}
	return s.FindOneByType(ctx, id, task.Type(s.task.Type))
}

func (s *workerTaskStore) QueryTaskList(context.Context, *task.Filter) (int64, []*task.Task, error) {
	if s.task == nil {
		return 0, nil, nil
	}
	return 1, []*task.Task{s.task}, nil
}

func (s *workerTaskStore) Update(_ context.Context, data *task.Task) error {
	s.task = data
	return nil
}

func (s *workerTaskStore) UpdateStatus(_ context.Context, _ int64, status int8) error {
	s.task.Status = status
	return nil
}

func (s *workerTaskStore) UpdateStatusFrom(_ context.Context, _ int64, _ task.Type, from []int8, status int8) (bool, error) {
	for _, candidate := range from {
		if s.task.Status == candidate {
			s.task.Status = status
			return true, nil
		}
	}
	return false, nil
}

func (s *workerTaskStore) UpdateStatusAndErrorFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8, taskError string) (bool, error) {
	updated, err := s.UpdateStatusFrom(ctx, id, typ, from, status)
	if updated {
		s.task.Errors = taskError
	}
	return updated, err
}

// deadlineContext is a context whose deadline a test moves while the worker
// runs; it never actually expires, so the worker's own budget check is what
// stops the run.
type deadlineContext struct {
	context.Context
	mu       sync.Mutex
	deadline time.Time
}

func newDeadlineContext(deadline time.Time) *deadlineContext {
	return &deadlineContext{Context: context.Background(), deadline: deadline}
}

func (c *deadlineContext) Deadline() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadline, true
}

func (c *deadlineContext) move(deadline time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = deadline
}

// hookedSender runs afterSend once its first delivery returned.
type hookedSender struct {
	workerSender
	afterSend func()
}

func (s *hookedSender) SendContext(ctx context.Context, to []string, subject, body string) error {
	err := s.workerSender.SendContext(ctx, to, subject, body)
	if s.afterSend != nil {
		hook := s.afterSend
		s.afterSend = nil
		hook()
	}
	return err
}

// recordingQueue records the continuations the handler enqueues.
type recordingQueue struct {
	tasks   []*asynq.Task
	options [][]asynq.Option
	err     error
}

func (q *recordingQueue) EnqueueContext(_ context.Context, t *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.tasks = append(q.tasks, t)
	q.options = append(q.options, opts)
	return &asynq.TaskInfo{}, nil
}

// option returns the value of the option of kind among opts, or nil.
func option(opts []asynq.Option, kind asynq.OptionType) any {
	for _, opt := range opts {
		if opt.Type() == kind {
			return opt.Value()
		}
	}
	return nil
}

// A run whose deadline draws too near to pace another delivery stops after
// the recipient it just recorded and asks for a continuation from there; the
// campaign stays in progress with its cursor on the next recipient.
func TestWorkerStopsBeforeTheRunDeadline(t *testing.T) {
	logtest.Discard(t)
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 0, "a@example.com", "b@example.com", "c@example.com")}
	ctx := newDeadlineContext(time.Now().Add(time.Hour))
	sender := &hookedSender{}
	// After the first delivery the deadline is closer than the interval and
	// the reserve together.
	sender.afterSend = func() { ctx.move(time.Now().Add(30 * time.Second)) }

	err := NewWorker(ctx, 7, store, sender).Start()
	var budget *RunBudgetExhausted
	if !errors.As(err, &budget) {
		t.Fatalf("Start error = %v, want the run budget exhausted", err)
	}
	if budget.Sent != 1 || budget.ResumeAt.Before(time.Now()) {
		t.Fatalf("continuation = %+v, want a resume after the one recipient sent", budget)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "a@example.com" {
		t.Fatalf("sent = %v, want only the first recipient", sender.sent)
	}
	if store.task.Status != task.StatusInProgress || store.task.Current != 1 {
		t.Fatalf("task = %+v, want it in progress at the second recipient", store.task)
	}
}

// A run that starts with no budget left sends nothing and hands the whole
// chunk to a fresh task: a delivery its deadline cut off would be retried
// with a backoff instead.
func TestWorkerWithoutBudgetSendsNothing(t *testing.T) {
	logtest.Discard(t)
	store := &workerTaskStore{task: newEmailTask(t, task.StatusPending, 0, "a@example.com")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sender := &workerSender{}

	err := NewWorker(ctx, 7, store, sender).Start()
	var budget *RunBudgetExhausted
	if !errors.As(err, &budget) || budget.Sent != 0 {
		t.Fatalf("Start error = %v, want a continuation from the first recipient", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v, want nothing", sender.sent)
	}
	if store.task.Status != task.StatusInProgress || store.task.Current != 0 {
		t.Fatalf("task = %+v, want it in progress at the first recipient", store.task)
	}
}

// A run without a deadline is never short of budget.
func TestWorkerWithoutDeadlineRunsToCompletion(t *testing.T) {
	store := &workerTaskStore{task: newEmailTask(t, task.StatusPending, 0, "a@example.com")}
	sender := &workerSender{}
	if err := NewWorker(context.Background(), 7, store, sender).Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.task.Status != task.StatusCompleted || len(sender.sent) != 1 {
		t.Fatalf("task = %+v, sent = %v", store.task, sender.sent)
	}
}

func newBatchHandler(store *workerTaskStore, queue *recordingQueue, sender mail.Sender) *BatchEmailHandler {
	h := NewBatchEmailHandler(Dependencies{
		Tasks:    store,
		Queue:    queue,
		Email:    func() config.EmailConfig { return config.EmailConfig{Platform: "smtp"} },
		SiteName: func() string { return "Panel" },
	})
	h.newSender = func(string, string, string) (mail.Sender, error) { return sender, nil }
	return h
}

// batchTask is the queue task of campaign 7, the one the worker store holds.
func batchTask() *asynq.Task {
	return asynq.NewTask(taskqueue.ScheduledBatchSendEmail, []byte(strconv.FormatInt(7, 10)))
}

// A run that stops for lack of budget is not a failed attempt: the handler
// answers success and queues the continuation at once, under a task id of
// the resume position and with the long campaign timeout, so the campaign
// carries on without a retry backoff and is never marked failed.
func TestProcessTaskContinuesAnExhaustedRunAtOnce(t *testing.T) {
	logtest.Discard(t)
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 2, "a@example.com", "b@example.com", "c@example.com", "d@example.com")}
	queue := &recordingQueue{}
	sender := &workerSender{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := newBatchHandler(store, queue, sender).ProcessTask(ctx, batchTask()); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if len(sender.sent) != 0 || store.task.Status != task.StatusInProgress || store.task.Current != 2 {
		t.Fatalf("sent = %v, task = %+v; want nothing sent and the campaign still in progress", sender.sent, store.task)
	}
	if len(queue.tasks) != 1 || queue.tasks[0].Type() != taskqueue.ScheduledBatchSendEmail || string(queue.tasks[0].Payload()) != "7" {
		t.Fatalf("queued = %+v, want one continuation of task 7", queue.tasks)
	}
	opts := queue.options[0]
	if id := option(opts, asynq.TaskIDOpt); id != "marketing-email-7-chunk-2" {
		t.Fatalf("continuation id = %v, want the resume position", id)
	}
	if timeout := option(opts, asynq.TimeoutOpt); timeout != taskqueue.BatchEmailTaskTimeout {
		t.Fatalf("continuation timeout = %v, want %v", timeout, taskqueue.BatchEmailTaskTimeout)
	}
	at, ok := option(opts, asynq.ProcessAtOpt).(time.Time)
	if !ok || at.After(time.Now().Add(time.Minute)) {
		t.Fatalf("continuation runs at %v, want at once", at)
	}
}

// A continuation another delivery of the same run already queued is this
// one: the id conflict is success.
func TestProcessTaskTreatsAQueuedContinuationAsDone(t *testing.T) {
	logtest.Discard(t)
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 0, "a@example.com")}
	queue := &recordingQueue{err: asynq.ErrTaskIDConflict}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := newBatchHandler(store, queue, &workerSender{}).ProcessTask(ctx, batchTask()); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
}

// A worker stopped from inside the process while its campaign is still
// active — the run's own deadline has not passed — continues from a
// follow-up task instead of failing the attempt.
func TestProcessTaskResumesAnInterruptedActiveCampaign(t *testing.T) {
	logtest.Discard(t)
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 0, "a@example.com", "b@example.com")}
	queue := &recordingQueue{}
	sender := &hookedSender{}
	// The worker is removed while its first delivery is out; the pause
	// before the second recipient then sees the cancelled context.
	sender.afterSend = func() { NewWorkerManager().RemoveWorker(7) }

	if err := newBatchHandler(store, queue, sender).ProcessTask(context.Background(), batchTask()); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if store.task.Status != task.StatusInProgress || store.task.Current != 1 {
		t.Fatalf("task = %+v, want the campaign in progress after one recipient", store.task)
	}
	if len(queue.tasks) != 1 || option(queue.options[0], asynq.TaskIDOpt) != "marketing-email-7-chunk-1" {
		t.Fatalf("queued = %+v, want the continuation from the second recipient", queue.options)
	}
}

// A campaign an administrator stopped is done when its worker is removed:
// nothing is queued and nothing is retried.
func TestProcessTaskFinishesACancelledCampaign(t *testing.T) {
	logtest.Discard(t)
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 0, "a@example.com", "b@example.com")}
	queue := &recordingQueue{}
	sender := &hookedSender{}
	sender.afterSend = func() {
		store.task.Status = task.StatusCancelled
		NewWorkerManager().RemoveWorker(7)
	}

	if err := newBatchHandler(store, queue, sender).ProcessTask(context.Background(), batchTask()); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if len(queue.tasks) != 0 {
		t.Fatalf("queued = %+v, want nothing for a cancelled campaign", queue.tasks)
	}
}

// An interruption never marks the campaign failed, whatever the attempt.
func TestHandleFailureKeepsAnInterruptedCampaign(t *testing.T) {
	store := &workerTaskStore{task: newEmailTask(t, task.StatusInProgress, 1, "a@example.com", "b@example.com")}
	h := newBatchHandler(store, &recordingQueue{}, &workerSender{})
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		if err := h.handleFailure(context.Background(), 7, cause); !errors.Is(err, cause) {
			t.Fatalf("handleFailure(%v) = %v, want the cause returned for a retry", cause, err)
		}
		if store.task.Status != task.StatusInProgress || store.updates != 0 {
			t.Fatalf("task = %+v after %v, want it untouched", store.task, cause)
		}
	}
}

// resolvingRecipients answers the accounts a scope selects today.
type resolvingRecipients struct {
	emails  []string
	err     error
	filters []user.EmailRecipientFilter
}

func (r *resolvingRecipients) QueryEmailRecipients(_ context.Context, filter *user.EmailRecipientFilter) ([]string, error) {
	r.filters = append(r.filters, *filter)
	return r.emails, r.err
}

// The audience is re-resolved when the run starts: a recorded recipient the
// scope no longer selects (a deleted account) is passed over without a
// delivery or an audit row, keeping its position so the cursor stays valid;
// the additional addresses are always sent to.
func TestWorkerSkipsRecipientsTheScopeNoLongerSelects(t *testing.T) {
	logtest.Discard(t)
	data := newEmailTask(t, task.StatusPending, 0, "a@example.com", "gone@example.com", "c@example.com")
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(data.Scope)); err != nil {
		t.Fatal(err)
	}
	scope.Type, scope.RegisterStartTime, scope.Interval = task.ScopeActive.Int8(), 1700000000, 1
	scope.Additional = []string{"extra@example.com"}
	encoded, _ := scope.Marshal()
	data.Scope = string(encoded)
	store := &workerTaskStore{task: data}
	sender := &workerSender{}
	logs := &workerLogStore{}
	recipients := &resolvingRecipients{emails: []string{"A@example.com", "c@example.com"}}

	err := NewWorker(context.Background(), 7, store, sender, WithMessageLogs(logs, "smtp"), WithRecipientResolver(recipients)).Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := []string{"a@example.com", "c@example.com", "extra@example.com"}
	if len(sender.sent) != len(want) {
		t.Fatalf("sent = %v, want %v", sender.sent, want)
	}
	for i := range want {
		if sender.sent[i] != want[i] {
			t.Fatalf("sent = %v, want %v", sender.sent, want)
		}
	}
	if len(logs.logs) != 3 || store.task.Current != 4 || store.task.Status != task.StatusCompleted {
		t.Fatalf("audit rows = %d, task = %+v; want three deliveries and the cursor past every position", len(logs.logs), store.task)
	}
	if len(recipients.filters) != 1 || recipients.filters[0].Scope != task.ScopeActive.Int8() || recipients.filters[0].RegisterStartTime != 1700000000 {
		t.Fatalf("resolved with %+v, want the campaign's scope", recipients.filters)
	}
}

// When the audience cannot be re-resolved the run is retried rather than
// sent to a possibly stale list.
func TestWorkerDoesNotSendWhenRecipientsCannotBeResolved(t *testing.T) {
	logtest.Discard(t)
	data := newEmailTask(t, task.StatusPending, 0, "a@example.com")
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(data.Scope)); err != nil {
		t.Fatal(err)
	}
	scope.Type = task.ScopeAll.Int8()
	encoded, _ := scope.Marshal()
	data.Scope = string(encoded)
	store := &workerTaskStore{task: data}
	sender := &workerSender{}
	down := errors.New("identity store down")
	err := NewWorker(context.Background(), 7, store, sender, WithRecipientResolver(&resolvingRecipients{err: down})).Start()
	if !errors.Is(err, down) || len(sender.sent) != 0 {
		t.Fatalf("Start error = %v, sent = %v; want the failure and no delivery", err, sender.sent)
	}
}

// The scope that selects nobody sends to the recorded additional addresses
// without asking the identity domain.
func TestWorkerDoesNotResolveTheSkipScope(t *testing.T) {
	data := newEmailTask(t, task.StatusPending, 0)
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(data.Scope)); err != nil {
		t.Fatal(err)
	}
	scope.Type, scope.Additional = task.ScopeSkip.Int8(), []string{"extra@example.com"}
	encoded, _ := scope.Marshal()
	data.Scope, data.Total = string(encoded), 1
	store := &workerTaskStore{task: data}
	sender := &workerSender{}
	recipients := &resolvingRecipients{err: errors.New("must not be asked")}
	if err := NewWorker(context.Background(), 7, store, sender, WithRecipientResolver(recipients)).Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(sender.sent) != 1 || len(recipients.filters) != 0 {
		t.Fatalf("sent = %v, resolved %d times; want the additional address sent without resolving", sender.sent, len(recipients.filters))
	}
}
