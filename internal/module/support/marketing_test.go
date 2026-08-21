package support_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	userEntity "github.com/perfect-panel/server/internal/module/identity/entity/user"
	taskEntity "github.com/perfect-panel/server/internal/module/platform/entity/task"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/internal/module/support"
)

type fakeTaskRepo struct {
	inserted      *taskEntity.Task
	statusUpdates []statusUpdate
	findOne       *taskEntity.Task
	statusCtxErr  error
}

func (f *fakeTaskRepo) Insert(_ context.Context, data *taskEntity.Task) error {
	data.Id = 77
	f.inserted = data
	return nil
}

func (f *fakeTaskRepo) FindOne(_ context.Context, _ int64) (*taskEntity.Task, error) {
	return f.findOne, nil
}

func (f *fakeTaskRepo) FindOneByType(_ context.Context, _ int64, typ taskEntity.Type) (*taskEntity.Task, error) {
	if f.findOne == nil || f.findOne.Type != int8(typ) {
		return nil, errors.New("task not found")
	}
	return f.findOne, nil
}

func (f *fakeTaskRepo) QueryTaskList(_ context.Context, _ *taskEntity.Filter) (int64, []*taskEntity.Task, error) {
	return 0, nil, nil
}

func (f *fakeTaskRepo) Update(_ context.Context, _ *taskEntity.Task) error { return nil }

func (f *fakeTaskRepo) UpdateActive(_ context.Context, data *taskEntity.Task) (bool, error) {
	f.findOne = data
	return true, nil
}

func (f *fakeTaskRepo) UpdateStatus(_ context.Context, id int64, status int8) error {
	f.statusUpdates = append(f.statusUpdates, statusUpdate{ticketID: id, status: uint8(status)})
	return nil
}

func (f *fakeTaskRepo) UpdateStatusFrom(_ context.Context, id int64, typ taskEntity.Type, from []int8, status int8) (bool, error) {
	if f.findOne == nil || f.findOne.Id != id || f.findOne.Type != int8(typ) {
		return false, nil
	}
	allowed := false
	for _, candidate := range from {
		allowed = allowed || f.findOne.Status == candidate
	}
	if !allowed {
		return false, nil
	}
	f.findOne.Status = status
	f.statusUpdates = append(f.statusUpdates, statusUpdate{ticketID: id, status: uint8(status)})
	return true, nil
}

func (f *fakeTaskRepo) UpdateStatusAndErrorFrom(ctx context.Context, id int64, typ taskEntity.Type, from []int8, status int8, taskError string) (bool, error) {
	f.statusCtxErr = ctx.Err()
	target := f.findOne
	if f.inserted != nil && f.inserted.Id == id && f.inserted.Type == int8(typ) {
		target = f.inserted
	}
	if target == nil || target.Id != id || target.Type != int8(typ) {
		return false, nil
	}
	allowed := false
	for _, candidate := range from {
		allowed = allowed || target.Status == candidate
	}
	if !allowed {
		return false, nil
	}
	target.Status = status
	target.Errors = taskError
	return true, nil
}

type fakeRecipients struct {
	emails []string
	count  int64
}

func (f fakeRecipients) QueryEmailRecipients(_ context.Context, _ *userEntity.EmailRecipientFilter) ([]string, error) {
	return f.emails, nil
}

func (f fakeRecipients) CountEmailRecipients(_ context.Context, _ *userEntity.EmailRecipientFilter) (int64, error) {
	return f.count, nil
}

type fakeQuotaTargets struct {
	ids []int64
}

func (f fakeQuotaTargets) QuerySubscribeIdsByFilter(_ context.Context, _ *usersub.SubscribeFilter) ([]int64, error) {
	return f.ids, nil
}

func (f fakeQuotaTargets) CountSubscribesByFilter(_ context.Context, _ *usersub.SubscribeFilter) (int64, error) {
	return int64(len(f.ids)), nil
}

type fakeMarketingQueue struct {
	emailTaskID int64
	processAt   time.Time
	quotaTaskID int64
	emailErr    error
	quotaErr    error
	onEmail     func()
	onQuota     func()
}

func (f *fakeMarketingQueue) EnqueueBatchEmail(_ context.Context, taskID int64, processAt time.Time) (string, error) {
	f.emailTaskID, f.processAt = taskID, processAt
	if f.onEmail != nil {
		f.onEmail()
	}
	return "queue-1", f.emailErr
}

func (f *fakeMarketingQueue) EnqueueQuota(_ context.Context, taskID int64) error {
	f.quotaTaskID = taskID
	if f.onQuota != nil {
		f.onQuota()
	}
	return f.quotaErr
}

type fakeStopper struct {
	stopped []int64
}

func (f *fakeStopper) StopBatchEmail(taskID int64) { f.stopped = append(f.stopped, taskID) }

type marketingFakes struct {
	tasks   *fakeTaskRepo
	queue   *fakeMarketingQueue
	stopper *fakeStopper
}

func newMarketingService(recipients fakeRecipients, targets fakeQuotaTargets) (support.Service, *marketingFakes) {
	fakes := &marketingFakes{
		tasks:   &fakeTaskRepo{findOne: &taskEntity.Task{Id: 42, Type: taskEntity.TypeEmail, Status: taskEntity.StatusPending}},
		queue:   &fakeMarketingQueue{},
		stopper: &fakeStopper{},
	}
	svc := support.New(support.Deps{
		Tasks:        fakes.tasks,
		Recipients:   recipients,
		QuotaTargets: targets,
		Queue:        fakes.queue,
		EmailStopper: fakes.stopper,
	})
	return svc, fakes
}

func TestCreateBatchSendEmailTaskRejectsEmptyScope(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{})

	err := svc.CreateBatchSendEmailTask(context.Background(), &dto.CreateBatchSendEmailTaskRequest{
		Subject: "subject", Content: "content", Scope: taskEntity.ScopeAll.Int8(),
	})
	if err == nil {
		t.Fatal("empty recipient list for a non-skip scope must be rejected")
	}
	if fakes.tasks.inserted != nil {
		t.Fatal("no task may be created when recipients are empty")
	}
}

func TestCreateBatchSendEmailTaskSkipScopeRequiresAdditional(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{})

	err := svc.CreateBatchSendEmailTask(context.Background(), &dto.CreateBatchSendEmailTaskRequest{
		Subject: "subject", Content: "content", Scope: taskEntity.ScopeSkip.Int8(),
	})
	if err == nil {
		t.Fatal("skip scope without additional addresses must be rejected")
	}
	if fakes.tasks.inserted != nil {
		t.Fatal("no task may be created without any recipient")
	}
}

func TestCreateBatchSendEmailTaskDedupesAndEnqueues(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{emails: []string{"a@x.com", "a@x.com", "b@x.com"}}, fakeQuotaTargets{})

	err := svc.CreateBatchSendEmailTask(context.Background(), &dto.CreateBatchSendEmailTaskRequest{
		Subject:    "subject",
		Content:    "content",
		Scope:      taskEntity.ScopeAll.Int8(),
		Additional: "b@x.com\nc@x.com",
	})
	if err != nil {
		t.Fatalf("CreateBatchSendEmailTask: %v", err)
	}
	got := fakes.tasks.inserted
	if got == nil || got.Type != taskEntity.TypeEmail {
		t.Fatalf("email task not created: %+v", got)
	}
	if got.Total != 3 {
		t.Fatalf("total = %d, want 3 (deduped across recipients and additional)", got.Total)
	}
	if fakes.queue.emailTaskID != 77 {
		t.Fatalf("enqueued task id = %d, want the created task id 77", fakes.queue.emailTaskID)
	}
	if fakes.queue.processAt.IsZero() {
		t.Fatal("batch email must be scheduled with a processAt time")
	}
}

func TestPreSendEmailCountMatchesDeduplicatedCampaignRecipients(t *testing.T) {
	svc, _ := newMarketingService(fakeRecipients{emails: []string{"a@x.com", "b@x.com"}}, fakeQuotaTargets{})
	resp, err := svc.GetPreSendEmailCount(context.Background(), &dto.GetPreSendEmailCountRequest{
		Scope: taskEntity.ScopeAll.Int8(), Additional: " b@x.com\r\nc@x.com ",
	})
	if err != nil {
		t.Fatalf("GetPreSendEmailCount: %v", err)
	}
	if resp.Count != 3 {
		t.Fatalf("pre-send count = %d, want 3", resp.Count)
	}
}

func TestCreateQuotaTaskRejectsNoSubscribers(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{})

	if err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{Days: 1}); err == nil {
		t.Fatal("quota task without matching subscribers must be rejected")
	}
	if fakes.tasks.inserted != nil {
		t.Fatal("no task may be created without targets")
	}
}

func TestCreateQuotaTaskEnqueuesWithTargets(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{ids: []int64{4, 5, 6}})

	if err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{Days: 7}); err != nil {
		t.Fatalf("CreateQuotaTask: %v", err)
	}
	got := fakes.tasks.inserted
	if got == nil || got.Type != taskEntity.TypeQuota || got.Total != 3 {
		t.Fatalf("unexpected quota task: %+v", got)
	}
	if fakes.queue.quotaTaskID != 77 {
		t.Fatalf("enqueued task id = %d, want 77", fakes.queue.quotaTaskID)
	}
}

func TestStopBatchSendEmailTaskStopsWorkerAndMarksTask(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{})

	if err := svc.StopBatchSendEmailTask(context.Background(), &dto.StopBatchSendEmailTaskRequest{Id: 42}); err != nil {
		t.Fatalf("StopBatchSendEmailTask: %v", err)
	}
	if len(fakes.stopper.stopped) != 1 || fakes.stopper.stopped[0] != 42 {
		t.Fatalf("worker not stopped: %+v", fakes.stopper.stopped)
	}
	if len(fakes.tasks.statusUpdates) != 1 || fakes.tasks.statusUpdates[0] != (statusUpdate{ticketID: 42, status: uint8(taskEntity.StatusCancelled)}) {
		t.Fatalf("task status must be set to cancelled: %+v", fakes.tasks.statusUpdates)
	}
}

func TestCreateMarketingTasksMarkEnqueueFailure(t *testing.T) {
	t.Run("email", func(t *testing.T) {
		svc, fakes := newMarketingService(fakeRecipients{emails: []string{"a@x.com"}}, fakeQuotaTargets{})
		fakes.queue.emailErr = errors.New("redis unavailable")
		err := svc.CreateBatchSendEmailTask(context.Background(), &dto.CreateBatchSendEmailTaskRequest{
			Subject: "subject", Content: "content", Scope: taskEntity.ScopeAll.Int8(),
		})
		if err == nil || fakes.tasks.inserted.Status != taskEntity.StatusEnqueueFailed || fakes.tasks.inserted.Errors == "" {
			t.Fatalf("enqueue failure was not persisted: task=%+v err=%v", fakes.tasks.inserted, err)
		}
	})

	t.Run("quota", func(t *testing.T) {
		svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{ids: []int64{4}})
		fakes.queue.quotaErr = errors.New("redis unavailable")
		err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{Days: 1})
		if err == nil || fakes.tasks.inserted.Status != taskEntity.StatusEnqueueFailed || fakes.tasks.inserted.Errors == "" {
			t.Fatalf("enqueue failure was not persisted: task=%+v err=%v", fakes.tasks.inserted, err)
		}
	})

	t.Run("started task wins enqueue response race", func(t *testing.T) {
		svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{ids: []int64{4}})
		fakes.queue.quotaErr = errors.New("redis response lost")
		fakes.queue.onQuota = func() { fakes.tasks.inserted.Status = taskEntity.StatusInProgress }
		err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{Days: 1})
		if err == nil {
			t.Fatal("enqueue response error must still be reported")
		}
		if fakes.tasks.inserted.Status != taskEntity.StatusInProgress {
			t.Fatalf("enqueue error overwrote a task that had already started: %+v", fakes.tasks.inserted)
		}
	})

	t.Run("failure state survives request cancellation", func(t *testing.T) {
		svc, fakes := newMarketingService(fakeRecipients{emails: []string{"a@x.com"}}, fakeQuotaTargets{})
		fakes.queue.emailErr = errors.New("redis unavailable")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := svc.CreateBatchSendEmailTask(ctx, &dto.CreateBatchSendEmailTaskRequest{
			Subject: "subject", Content: "content", Scope: taskEntity.ScopeAll.Int8(),
		})
		if err == nil || fakes.tasks.inserted.Status != taskEntity.StatusEnqueueFailed {
			t.Fatalf("enqueue failure was not recorded after cancellation: task=%+v err=%v", fakes.tasks.inserted, err)
		}
		if fakes.tasks.statusCtxErr != nil {
			t.Fatalf("enqueue failure update inherited canceled request context: %v", fakes.tasks.statusCtxErr)
		}
	})
}

func TestCreateMarketingTasksValidateDangerousInputs(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{emails: []string{"a@x.com"}}, fakeQuotaTargets{ids: []int64{4}})
	if err := svc.CreateBatchSendEmailTask(context.Background(), &dto.CreateBatchSendEmailTaskRequest{
		Subject: "subject", Content: "content", Scope: taskEntity.ScopeSkip.Int8(), Additional: "not-an-email",
	}); err == nil {
		t.Fatal("invalid additional email must be rejected")
	}
	if err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{}); err == nil {
		t.Fatal("quota task without an action must be rejected")
	}
	if err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{GiftValue: 10}); err == nil {
		t.Fatal("gift value without a gift type must be rejected")
	}
	if fakes.tasks.inserted != nil {
		t.Fatalf("invalid requests must not create tasks: %+v", fakes.tasks.inserted)
	}
}
