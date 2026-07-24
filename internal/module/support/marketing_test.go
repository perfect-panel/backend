package support_test

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	taskEntity "github.com/perfect-panel/server/internal/model/entity/task"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/module/support"
)

type fakeTaskRepo struct {
	inserted      *taskEntity.Task
	statusUpdates []statusUpdate
	findOne       *taskEntity.Task
}

func (f *fakeTaskRepo) Insert(_ context.Context, data *taskEntity.Task) error {
	data.Id = 77
	f.inserted = data
	return nil
}

func (f *fakeTaskRepo) FindOne(_ context.Context, _ int64) (*taskEntity.Task, error) {
	return f.findOne, nil
}

func (f *fakeTaskRepo) FindOneByType(_ context.Context, _ int64, _ taskEntity.Type) (*taskEntity.Task, error) {
	return f.findOne, nil
}

func (f *fakeTaskRepo) QueryTaskList(_ context.Context, _ *taskEntity.Filter) (int64, []*taskEntity.Task, error) {
	return 0, nil, nil
}

func (f *fakeTaskRepo) Update(_ context.Context, _ *taskEntity.Task) error { return nil }

func (f *fakeTaskRepo) UpdateStatus(_ context.Context, id int64, status int8) error {
	f.statusUpdates = append(f.statusUpdates, statusUpdate{ticketID: id, status: uint8(status)})
	return nil
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

func (f fakeQuotaTargets) QuerySubscribeIdsByFilter(_ context.Context, _ *userEntity.SubscribeFilter) ([]int64, error) {
	return f.ids, nil
}

func (f fakeQuotaTargets) CountSubscribesByFilter(_ context.Context, _ *userEntity.SubscribeFilter) (int64, error) {
	return int64(len(f.ids)), nil
}

type fakeMarketingQueue struct {
	emailTaskID int64
	processAt   time.Time
	quotaTaskID int64
}

func (f *fakeMarketingQueue) EnqueueBatchEmail(_ context.Context, taskID int64, processAt time.Time) (string, error) {
	f.emailTaskID, f.processAt = taskID, processAt
	return "queue-1", nil
}

func (f *fakeMarketingQueue) EnqueueQuota(_ context.Context, taskID int64) error {
	f.quotaTaskID = taskID
	return nil
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
	fakes := &marketingFakes{tasks: &fakeTaskRepo{}, queue: &fakeMarketingQueue{}, stopper: &fakeStopper{}}
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
		Scope: taskEntity.ScopeAll.Int8(),
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
		Scope: taskEntity.ScopeSkip.Int8(),
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

func TestCreateQuotaTaskRejectsNoSubscribers(t *testing.T) {
	svc, fakes := newMarketingService(fakeRecipients{}, fakeQuotaTargets{})

	if err := svc.CreateQuotaTask(context.Background(), &dto.CreateQuotaTaskRequest{}); err == nil {
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
	if len(fakes.tasks.statusUpdates) != 1 || fakes.tasks.statusUpdates[0] != (statusUpdate{ticketID: 42, status: 2}) {
		t.Fatalf("task status must be set to 2 (stopped): %+v", fakes.tasks.statusUpdates)
	}
}
