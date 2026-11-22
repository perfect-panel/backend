package marketing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The marketing service runs over the platform's task repository on the
// harness database. The recipient port selects a@example.com and
// b@example.com (twice), the target port the user subscriptions 11 and 12.

var (
	_ EmailRecipientReader = (*supporttest.Recipients)(nil)
	_ SubscriptionSelector = (*supporttest.QuotaTargets)(nil)
	_ Queue                = (*supporttest.Queue)(nil)
	_ BatchEmailStopper    = (*supporttest.Stopper)(nil)
)

type world struct {
	env        *supporttest.Env
	svc        *Service
	recipients *supporttest.Recipients
	targets    *supporttest.QuotaTargets
	queue      *supporttest.Queue
	stopper    *supporttest.Stopper
	audit      *supporttest.AuditLog
}

func newWorld(t *testing.T) *world {
	t.Helper()
	logtest.Discard(t)
	w := &world{
		env:        supporttest.New(t),
		recipients: &supporttest.Recipients{Emails: []string{"a@example.com", "b@example.com", "b@example.com"}},
		targets:    &supporttest.QuotaTargets{IDs: []int64{11, 12}},
		queue:      &supporttest.Queue{},
		stopper:    &supporttest.Stopper{},
		audit:      &supporttest.AuditLog{},
	}
	w.svc = NewService(w.env.Tasks, w.recipients, w.targets, w.queue, w.stopper, w.audit)
	return w
}

// nothingRecorded fails the test if a task was stored, queued or stopped.
func (w *world) nothingRecorded(t *testing.T) {
	t.Helper()
	if tasks := w.env.AllTasks(t); len(tasks) != 0 || len(w.queue.Emails)+len(w.queue.Quotas)+len(w.stopper.Stopped) != 0 {
		t.Fatalf("tasks %+v, queued %+v %v, stopped %v, want nothing", tasks, w.queue.Emails, w.queue.Quotas, w.stopper.Stopped)
	}
}

// refused fails the test unless err carries code with a message containing
// msg.
func refused(t *testing.T, err error, code uint32, msg string) {
	t.Helper()
	var coded *xerr.CodeError
	if !errors.As(err, &coded) || coded.GetErrCode() != code || !strings.Contains(coded.GetErrMsg(), msg) {
		t.Fatalf("error = %v, want code %d with a message containing %q", err, code, msg)
	}
}

func campaign(mutate func(*dto.CreateBatchSendEmailTaskRequest)) *dto.CreateBatchSendEmailTaskRequest {
	req := &dto.CreateBatchSendEmailTaskRequest{Subject: "news", Content: "<p>v2</p>", Scope: task.ScopeAll.Int8()}
	if mutate != nil {
		mutate(req)
	}
	return req
}

// A campaign request the service cannot run is refused with a message for
// the admin panel, and no campaign is recorded.
func TestCampaignRequestsAreValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *dto.CreateBatchSendEmailTaskRequest
		msg  string
	}{
		{"no request", nil, "request is required"},
		{"blank subject", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Subject = "  " }), "email subject and content are required"},
		{"blank content", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Content = "\n" }), "email subject and content are required"},
		{"scope below range", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Scope = 0 }), "invalid email scope"},
		{"scope above range", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Scope = 6 }), "invalid email scope"},
		{"registration window reversed", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.RegisterStartTime, r.RegisterEndTime = 20, 10 }),
			"register_start_time must not be after register_end_time"},
		{"registration before 1970", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.RegisterStartTime = -1 }), "registration timestamps must not be negative"},
		{"schedule before 1970", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Scheduled = -1 }), "scheduled must not be negative"},
		{"additional address invalid", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Additional = "c@example.com\nnot-an-email" }),
			"invalid additional email address: not-an-email"},
		// A display name would be dropped from the address actually sent to.
		{"additional address with a name", campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Additional = "Carol <c@example.com>" }),
			"invalid additional email address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			refused(t, w.svc.CreateBatchSendEmailTask(context.Background(), tc.req), xerr.ERROR, tc.msg)
			w.nothingRecorded(t)
		})
	}
}

// A campaign records who it goes to, when and how fast, and who created it;
// it is handed to the queue for the time it is to run.
func TestCampaignIsRecordedAndQueued(t *testing.T) {
	w := newWorld(t)
	before := time.Now()
	err := w.svc.CreateBatchSendEmailTask(supporttest.Context(), campaign(func(r *dto.CreateBatchSendEmailTaskRequest) {
		r.Scope = task.ScopeActive.Int8()
		r.RegisterStartTime, r.RegisterEndTime = 1700000000, 1760000000
		r.Additional = " B@example.com \r\nc@example.com\n\nc@example.com"
		r.Interval, r.Limit = 5, 100
	}))
	if err != nil {
		t.Fatal(err)
	}
	row := w.env.ReloadTask(t, 1)
	scope := supporttest.EmailScope(t, row)
	if row.Type != int8(task.TypeEmail) || row.Status != task.StatusPending || row.Total != 3 || row.Current != 0 {
		t.Fatalf("campaign = %+v, want a pending email task to three addresses", row)
	}
	// The selected recipients and the additional addresses are each sent to
	// once; the additional ones are kept, normalized, apart.
	if !reflect.DeepEqual(scope.Recipients, []string{"a@example.com", "b@example.com"}) || !reflect.DeepEqual(scope.Additional, []string{"b@example.com", "c@example.com"}) {
		t.Fatalf("recipients %v, additional %v", scope.Recipients, scope.Additional)
	}
	if scope.Type != task.ScopeActive.Int8() || scope.RegisterStartTime != 1700000000 || scope.RegisterEndTime != 1760000000 || scope.Interval != 5 || scope.Limit != 100 {
		t.Fatalf("scope = %+v", scope)
	}
	if scope.ClientIP != supporttest.ClientIP || scope.UserAgent != supporttest.UserAgent || scope.ActorID != supporttest.ActorID {
		t.Fatalf("request metadata = %+v, want the administrator's request", scope.Metadata)
	}
	var content task.EmailContent
	if err := content.Unmarshal([]byte(row.Content)); err != nil || content.Subject != "news" || content.Content != "<p>v2</p>" {
		t.Fatalf("content = %+v (err %v)", content, err)
	}
	if want := []supporttest.QueuedEmail{{TaskID: 1, ProcessAt: time.Unix(scope.Scheduled, 0)}}; len(w.queue.Emails) != 1 || w.queue.Emails[0].TaskID != 1 ||
		w.queue.Emails[0].ProcessAt.Unix() != want[0].ProcessAt.Unix() {
		t.Fatalf("queued = %+v, want %+v", w.queue.Emails, want)
	}
	// Unscheduled, the campaign runs ten seconds after it was created.
	if at := w.queue.Emails[0].ProcessAt; at.Before(before.Add(10*time.Second)) || at.After(time.Now().Add(10*time.Second)) {
		t.Fatalf("runs at %v, want ten seconds after %v", at, before)
	}
	if want := []int8{task.ScopeActive.Int8()}; len(w.recipients.Queried) != 1 || w.recipients.Queried[0].Scope != want[0] ||
		w.recipients.Queried[0].RegisterStartTime != 1700000000 || w.recipients.Queried[0].RegisterEndTime != 1760000000 {
		t.Fatalf("recipient selection = %+v", w.recipients.Queried)
	}
}

// A scheduled campaign runs at its time, or at once when that time has
// passed.
func TestCampaignRunsAtItsSchedule(t *testing.T) {
	future := time.Now().Add(time.Hour).Truncate(time.Second)
	for _, tc := range []struct {
		name      string
		scheduled int64
		want      func() time.Time
	}{
		{"future", future.Unix(), func() time.Time { return future }},
		{"past", 1600000000, time.Now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			if err := w.svc.CreateBatchSendEmailTask(context.Background(), campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Scheduled = tc.scheduled })); err != nil {
				t.Fatal(err)
			}
			at, want := w.queue.Emails[0].ProcessAt, tc.want()
			if at.Sub(want).Abs() > 2*time.Second || supporttest.EmailScope(t, w.env.ReloadTask(t, 1)).Scheduled != at.Unix() {
				t.Fatalf("runs at %v, want %v, as recorded", at, want)
			}
		})
	}
}

// The skip scope selects nobody: the additional addresses are the whole
// campaign, and a campaign to nobody is refused.
func TestCampaignNeedsRecipients(t *testing.T) {
	w := newWorld(t)
	w.recipients.Emails = nil
	refused(t, w.svc.CreateBatchSendEmailTask(context.Background(), campaign(nil)), xerr.ERROR, "No email addresses found for the campaign")
	w.nothingRecorded(t)

	if err := w.svc.CreateBatchSendEmailTask(context.Background(), campaign(func(r *dto.CreateBatchSendEmailTaskRequest) {
		r.Scope, r.Additional = task.ScopeSkip.Int8(), "c@example.com\nd@example.com"
	})); err != nil {
		t.Fatal(err)
	}
	if row := w.env.ReloadTask(t, 1); row.Total != 2 {
		t.Fatalf("campaign = %+v, want the two additional addresses", row)
	}
}

// startedWhileQueueFails makes the queue lose its answer after a worker
// already picked the task up and started it.
func startedWhileQueueFails(err error) func(t *testing.T, w *world) {
	return func(t *testing.T, w *world) {
		w.queue.Err = err
		w.queue.Received = func(id int64) {
			if err := w.env.Tasks.UpdateStatus(context.Background(), id, task.StatusInProgress); err != nil {
				t.Error(err)
			}
		}
	}
}

// A failure to select the recipients, to record the campaign or to queue it
// is reported; a campaign the queue refused is marked so rather than left
// pending, unless a worker already started it.
func TestCampaignFailures(t *testing.T) {
	down := errors.New("redis down")
	for _, tc := range []struct {
		name       string
		setup      func(t *testing.T, w *world)
		code       uint32
		wantStatus int8 // of the recorded campaign; -1 when none is recorded
	}{
		{"recipients unavailable", func(_ *testing.T, w *world) { w.recipients.Err = errors.New("identity store down") }, xerr.DatabaseQueryError, -1},
		{"not recorded", func(t *testing.T, w *world) { w.env.Refuse(t, "create", "task", 0, errors.New("disk full")) }, xerr.DatabaseInsertError, -1},
		{"not queued", func(_ *testing.T, w *world) { w.queue.Err = down }, xerr.QueueEnqueueError, task.StatusEnqueueFailed},
		{"not queued but started", startedWhileQueueFails(down), xerr.QueueEnqueueError, task.StatusInProgress},
		// The failure cannot be recorded either: the campaign stays pending,
		// and the enqueue error is still what the admin sees.
		{"not queued, failure not recorded", func(t *testing.T, w *world) {
			w.queue.Err = down
			w.env.Refuse(t, "update", "task", 0, errors.New("disk full"))
		}, xerr.QueueEnqueueError, task.StatusPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(t, w)
			err := w.svc.CreateBatchSendEmailTask(context.Background(), campaign(nil))
			refused(t, err, tc.code, "")
			tasks := w.env.AllTasks(t)
			if tc.wantStatus < 0 {
				if len(tasks) != 0 || len(w.queue.Emails) != 0 {
					t.Fatalf("tasks %+v, queued %+v, want nothing", tasks, w.queue.Emails)
				}
				return
			}
			if len(tasks) != 1 || tasks[0].Status != tc.wantStatus {
				t.Fatalf("tasks = %+v, want one in status %d", tasks, tc.wantStatus)
			}
			if failed := tc.wantStatus == task.StatusEnqueueFailed; failed != (tasks[0].Errors == "enqueue email task: redis down") {
				t.Fatalf("errors = %q", tasks[0].Errors)
			}
		})
	}
}

func TestPreSendCount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     *dto.GetPreSendEmailCountRequest
		count   int64
		msg     string // the refusal, if any
		counted bool   // answered by a count, not a selection
	}{
		{"count", &dto.GetPreSendEmailCountRequest{Scope: task.ScopeExpired.Int8(), RegisterEndTime: 1760000000}, 3, "", true},
		// Additional addresses are counted once, even when selected too.
		{"with additional addresses", &dto.GetPreSendEmailCountRequest{Scope: task.ScopeAll.Int8(), Additional: "B@example.com\nz@example.com"}, 3, "", false},
		{"no request", nil, 0, "invalid email scope", false},
		{"unknown scope", &dto.GetPreSendEmailCountRequest{Scope: 9}, 0, "invalid email scope", false},
		{"registration window reversed", &dto.GetPreSendEmailCountRequest{Scope: 1, RegisterStartTime: 5, RegisterEndTime: 4}, 0, "register_start_time must not be after register_end_time", false},
		{"registration before 1970", &dto.GetPreSendEmailCountRequest{Scope: 1, RegisterEndTime: -5}, 0, "registration timestamps must not be negative", false},
		{"additional address invalid", &dto.GetPreSendEmailCountRequest{Scope: 1, Additional: "@example.com"}, 0, "invalid additional email address", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			resp, err := w.svc.GetPreSendEmailCount(context.Background(), tc.req)
			if tc.msg != "" {
				refused(t, err, xerr.ERROR, tc.msg)
				return
			}
			if err != nil || resp.Count != tc.count || (len(w.recipients.Counted) == 1) != tc.counted || len(w.recipients.Queried)+len(w.recipients.Counted) != 1 {
				t.Fatalf("count = %+v (err %v), counted %+v, queried %+v", resp, err, w.recipients.Counted, w.recipients.Queried)
			}
			filter := append(w.recipients.Counted, w.recipients.Queried...)[0]
			if filter.Scope != tc.req.Scope || filter.RegisterEndTime != tc.req.RegisterEndTime {
				t.Fatalf("recipient filter = %+v, want the request's", filter)
			}
		})
	}
	for _, additional := range []string{"", "c@example.com"} {
		w := newWorld(t)
		w.recipients.Err = errors.New("identity store down")
		_, err := w.svc.GetPreSendEmailCount(context.Background(), &dto.GetPreSendEmailCountRequest{Scope: 1, Additional: additional})
		refused(t, err, xerr.ERROR, "Failed to count emails")
	}
}

// seedTasks stores two campaigns (1 pending to everyone, 2 completed to the
// active users, with a recorded failure) and a quota task (3).
func seedTasks(t *testing.T, env *supporttest.Env) {
	t.Helper()
	env.EmailTask(t, supporttest.EmailCampaign{Subject: "welcome", Content: "hi", Scope: task.ScopeAll.Int8(), Recipients: []string{"a@example.com"}, Total: 1,
		Errors: "legacy error text"})
	env.EmailTask(t, supporttest.EmailCampaign{Subject: "renew", Content: "renew now", Scope: task.ScopeActive.Int8(), Recipients: []string{"a@example.com", "b@example.com"},
		Additional: []string{"c@example.com"}, Status: task.StatusCompleted, Total: 3, Current: 3})
	env.TaskFailure(t, 2, 1, "b@example.com", "mailbox full", 1758000100)
	env.TaskFailure(t, 2, 0, "a@example.com", "unknown user", 1758000000)
	env.QuotaTask(t, supporttest.QuotaCampaign{Objects: []int64{11}, Days: 7})
}

func campaignIDs(list []dto.BatchSendEmailTask) []int64 {
	out := []int64{}
	for _, item := range list {
		out = append(out, item.Id)
	}
	return out
}

func statusPtr(v uint8) *uint8 { return &v }
func scopePtr(v int8) *int8    { return &v }

// The campaign list shows only campaigns, newest first, narrowed by status
// and scope; a campaign's recorded failures replace its legacy error text.
func TestCampaignListFiltersAndPages(t *testing.T) {
	w := newWorld(t)
	seedTasks(t, w.env)
	for _, tc := range []struct {
		name  string
		req   *dto.GetBatchSendEmailTaskListRequest
		total int64
		ids   []int64
	}{
		{"defaults", nil, 2, []int64{2, 1}},
		{"completed", &dto.GetBatchSendEmailTaskListRequest{Status: statusPtr(uint8(task.StatusCompleted))}, 1, []int64{2}},
		{"to everyone", &dto.GetBatchSendEmailTaskListRequest{Scope: scopePtr(task.ScopeAll.Int8())}, 1, []int64{1}},
		{"second page", &dto.GetBatchSendEmailTaskListRequest{Page: 2, Size: 1}, 2, []int64{1}},
		{"past the end", &dto.GetBatchSendEmailTaskListRequest{Page: 3, Size: 1}, 2, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := w.svc.GetBatchSendEmailTaskList(context.Background(), tc.req)
			if err != nil || resp.Total != tc.total || resp.List == nil || !reflect.DeepEqual(campaignIDs(resp.List), tc.ids) {
				t.Fatalf("list = %+v (err %v), want %d %v", resp, err, tc.total, tc.ids)
			}
		})
	}
	resp, err := w.svc.GetBatchSendEmailTaskList(context.Background(), &dto.GetBatchSendEmailTaskListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	renew, welcome := resp.List[0], resp.List[1]
	if renew.Recipients != "a@example.com\nb@example.com" || renew.RecipientCount != 3 || renew.Additional != "c@example.com" || renew.Scope != task.ScopeActive.Int8() || renew.Current != 3 ||
		renew.Errors != `[{"error":"unknown user","email":"a@example.com","time":1758000000},{"error":"mailbox full","email":"b@example.com","time":1758000100}]` {
		t.Fatalf("renew campaign = %+v", renew)
	}
	if welcome.Subject != "welcome" || welcome.RecipientCount != 1 || welcome.Errors != "legacy error text" {
		t.Fatalf("welcome campaign = %+v", welcome)
	}
}

// A campaign's list entry shows a bounded sample of its recipients and how
// many more there are, with the whole audience as a count: a campaign to a
// hundred thousand accounts ships no address list with every page.
func TestCampaignListBoundsTheRecipientList(t *testing.T) {
	w := newWorld(t)
	recipients := make([]string, 0, 25)
	for i := range 25 {
		recipients = append(recipients, fmt.Sprintf("user%02d@example.com", i))
	}
	w.env.EmailTask(t, supporttest.EmailCampaign{Subject: "s", Content: "c", Scope: task.ScopeAll.Int8(), Recipients: recipients, Additional: []string{"x@example.com"}, Total: 26})
	resp, err := w.svc.GetBatchSendEmailTaskList(context.Background(), nil)
	if err != nil || len(resp.List) != 1 {
		t.Fatalf("list = %+v (err %v)", resp, err)
	}
	got := resp.List[0]
	if got.RecipientCount != 26 {
		t.Fatalf("recipient count = %d, want 26", got.RecipientCount)
	}
	lines := strings.Split(got.Recipients, "\n")
	if len(lines) != maxListedRecipients+1 || lines[0] != "user00@example.com" || lines[maxListedRecipients-1] != "user19@example.com" || lines[maxListedRecipients] != "… and 5 more" {
		t.Fatalf("recipients = %q, want the first %d and the rest counted", got.Recipients, maxListedRecipients)
	}
}

// Creating and stopping a campaign, and creating a quota task, leave an
// audit trail with the acting administrator and the task, naming no
// address.
func TestMarketingActionsAreAudited(t *testing.T) {
	w := newWorld(t)
	ctx := supporttest.Context()
	if err := w.svc.CreateBatchSendEmailTask(ctx, campaign(func(r *dto.CreateBatchSendEmailTaskRequest) { r.Additional = "c@example.com" })); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.StopBatchSendEmailTask(ctx, &dto.StopBatchSendEmailTaskRequest{Id: 1}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.CreateQuotaTask(ctx, quota(func(r *dto.CreateQuotaTaskRequest) { r.Subscribers = []int64{3} })); err != nil {
		t.Fatal(err)
	}
	actions := w.audit.Actions(t)
	want := []struct {
		action, object string
		id             int64
		detail         string
	}{
		{"marketing.campaign.create", "email_task", 1, "recipients=2 additional=1"},
		{"marketing.campaign.stop", "email_task", 1, ""},
		{"marketing.quota.create", "quota_task", 2, "subscriptions=2 reset_traffic=false days=7"},
	}
	if len(actions) != len(want) {
		t.Fatalf("audited %d actions, want %d: %+v", len(actions), len(want), actions)
	}
	for i, tc := range want {
		got := actions[i]
		if got.Action != tc.action || got.Object != tc.object || got.ObjectID != tc.id || !strings.Contains(got.Detail, tc.detail) ||
			got.ActorID != supporttest.ActorID || got.Source != log.AdminActionSourceHTTP || strings.Contains(got.Detail, "@") {
			t.Fatalf("action %d = %+v, want %+v without any address", i, got, tc)
		}
	}
}

// A task whose stored scope or content cannot be read fails its list rather
// than showing a blank task.
func TestTaskListsRefuseUnreadableTasks(t *testing.T) {
	lists := map[string]struct {
		id   int64
		read func(s *Service) error
	}{
		"campaign list": {1, func(s *Service) error {
			_, err := s.GetBatchSendEmailTaskList(context.Background(), nil)
			return err
		}},
		"quota list": {3, func(s *Service) error {
			_, err := s.QueryQuotaTaskList(context.Background(), nil)
			return err
		}},
	}
	for name, list := range lists {
		for _, column := range []string{"scope", "content"} {
			t.Run(name+" "+column, func(t *testing.T) {
				w := newWorld(t)
				seedTasks(t, w.env)
				if err := w.env.DB.Exec("UPDATE task SET "+column+" = ? WHERE id = ?", "{not json", list.id).Error; err != nil {
					t.Fatal(err)
				}
				refused(t, list.read(w.svc), xerr.DatabaseQueryError, "Database query error")
			})
		}
	}
}

func TestCampaignStatus(t *testing.T) {
	w := newWorld(t)
	seedTasks(t, w.env)
	ctx := context.Background()
	for id, want := range map[int64]dto.GetBatchSendEmailTaskStatusResponse{
		1: {Status: uint8(task.StatusPending), Total: 1, Errors: "legacy error text"},
		2: {Status: uint8(task.StatusCompleted), Current: 3, Total: 3,
			Errors: `[{"error":"unknown user","email":"a@example.com","time":1758000000},{"error":"mailbox full","email":"b@example.com","time":1758000100}]`},
	} {
		got, err := w.svc.GetBatchSendEmailTaskStatus(ctx, &dto.GetBatchSendEmailTaskStatusRequest{Id: id})
		if err != nil || *got != want {
			t.Fatalf("status of %d = %+v (err %v), want %+v", id, got, err, want)
		}
	}
	for _, req := range []*dto.GetBatchSendEmailTaskStatusRequest{nil, {Id: 0}, {Id: -2}} {
		_, err := w.svc.GetBatchSendEmailTaskStatus(ctx, req)
		refused(t, err, xerr.ERROR, "invalid task id")
	}
	// The quota task is not a campaign.
	for _, id := range []int64{3, 9} {
		_, err := w.svc.GetBatchSendEmailTaskStatus(ctx, &dto.GetBatchSendEmailTaskStatusRequest{Id: id})
		refused(t, err, xerr.DatabaseQueryError, "Database query error")
	}
}

// A pending or running campaign can be stopped: it is cancelled and its
// worker stopped at once. A finished campaign, or a quota task, is not
// stoppable and stays as it was.
func TestStoppingCampaigns(t *testing.T) {
	for _, tc := range []struct {
		status  int8
		stopped bool
	}{
		{task.StatusPending, true},
		{task.StatusInProgress, true},
		{task.StatusCompleted, false},
		{task.StatusFailed, false},
		{task.StatusCancelled, false},
		{task.StatusEnqueueFailed, false},
	} {
		w := newWorld(t)
		id := w.env.EmailTask(t, supporttest.EmailCampaign{Subject: "s", Content: "c", Scope: 1, Status: tc.status})
		err := w.svc.StopBatchSendEmailTask(context.Background(), &dto.StopBatchSendEmailTaskRequest{Id: id})
		got := w.env.ReloadTask(t, id).Status
		if tc.stopped {
			if err != nil || got != task.StatusCancelled || !reflect.DeepEqual(w.stopper.Stopped, []int64{id}) {
				t.Fatalf("status %d: error %v, now %d, stopped %v, want it cancelled and its worker stopped", tc.status, err, got, w.stopper.Stopped)
			}
			continue
		}
		refused(t, err, xerr.ERROR, "email task is not stoppable")
		if got != tc.status || len(w.stopper.Stopped) != 0 {
			t.Fatalf("status %d: now %d, stopped %v, want it untouched", tc.status, got, w.stopper.Stopped)
		}
	}

	w := newWorld(t)
	quota := w.env.QuotaTask(t, supporttest.QuotaCampaign{Objects: []int64{11}, Days: 1})
	refused(t, w.svc.StopBatchSendEmailTask(context.Background(), &dto.StopBatchSendEmailTaskRequest{Id: quota}), xerr.ERROR, "email task is not stoppable")
	for _, req := range []*dto.StopBatchSendEmailTaskRequest{nil, {Id: 0}} {
		refused(t, w.svc.StopBatchSendEmailTask(context.Background(), req), xerr.ERROR, "invalid task id")
	}
	w.env.Refuse(t, "update", "task", 0, errors.New("disk full"))
	running := w.env.EmailTask(t, supporttest.EmailCampaign{Subject: "s", Content: "c", Scope: 1, Status: task.StatusInProgress})
	refused(t, w.svc.StopBatchSendEmailTask(context.Background(), &dto.StopBatchSendEmailTaskRequest{Id: running}), xerr.DatabaseUpdateError, "")
	if len(w.stopper.Stopped) != 0 {
		t.Fatalf("stopped %v, want the worker left running when the cancel was not recorded", w.stopper.Stopped)
	}
}

// Without a worker manager the campaign is still cancelled; a worker that
// runs later sees the cancellation.
func TestStoppingWithoutWorkerManager(t *testing.T) {
	logtest.Discard(t)
	env := supporttest.New(t)
	svc := NewService(env.Tasks, nil, nil, nil, nil, nil)
	id := env.EmailTask(t, supporttest.EmailCampaign{Subject: "s", Content: "c", Scope: 1})
	if err := svc.StopBatchSendEmailTask(context.Background(), &dto.StopBatchSendEmailTaskRequest{Id: id}); err != nil {
		t.Fatal(err)
	}
	if got := env.ReloadTask(t, id).Status; got != task.StatusCancelled {
		t.Fatalf("status = %d, want cancelled", got)
	}
}

func quota(mutate func(*dto.CreateQuotaTaskRequest)) *dto.CreateQuotaTaskRequest {
	req := &dto.CreateQuotaTaskRequest{Days: 7}
	if mutate != nil {
		mutate(req)
	}
	return req
}

// A quota task request that does nothing, or does something undefined, is
// refused before any subscription is selected.
func TestQuotaTaskRequestsAreValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *dto.CreateQuotaTaskRequest
		msg  string
	}{
		{"no request", nil, "request is required"},
		{"window reversed", quota(func(r *dto.CreateQuotaTaskRequest) { r.StartTime, r.EndTime = 20, 10 }), "start_time must not be after end_time"},
		{"window before 1970", quota(func(r *dto.CreateQuotaTaskRequest) { r.EndTime = -1 }), "invalid quota task filter"},
		{"subscriber id not positive", quota(func(r *dto.CreateQuotaTaskRequest) { r.Subscribers = []int64{3, 0} }), "invalid quota task filter"},
		{"no action", quota(func(r *dto.CreateQuotaTaskRequest) { r.Days = 0 }), "at least one quota action is required"},
		{"days overflow", quota(func(r *dto.CreateQuotaTaskRequest) { r.Days = math.MaxUint64 }), "days is too large"},
		{"gift type without value", quota(func(r *dto.CreateQuotaTaskRequest) { r.GiftType = 1 }), "gift_type requires a positive gift_value"},
		{"gift value without type", quota(func(r *dto.CreateQuotaTaskRequest) { r.GiftValue = 100 }), "gift_type must be fixed or ratio when gift_value is set"},
		{"gift of an unknown type", quota(func(r *dto.CreateQuotaTaskRequest) { r.GiftType, r.GiftValue = 3, 100 }), "gift_type must be fixed or ratio when gift_value is set"},
		{"gift value overflow", quota(func(r *dto.CreateQuotaTaskRequest) { r.GiftType, r.GiftValue = 1, math.MaxUint64 }), "gift_value is too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			refused(t, w.svc.CreateQuotaTask(context.Background(), tc.req), xerr.ERROR, tc.msg)
			w.nothingRecorded(t)
			if len(w.targets.Queried) != 0 {
				t.Fatalf("selected %+v, want nothing selected", w.targets.Queried)
			}
		})
	}
}

// Each quota action is a task of its own: a traffic reset, extra days, or a
// fixed or proportional gift.
func TestQuotaTaskIsRecordedAndQueued(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     *dto.CreateQuotaTaskRequest
		content task.QuotaContent
	}{
		{"traffic reset", &dto.CreateQuotaTaskRequest{ResetTraffic: true}, task.QuotaContent{ResetTraffic: true}},
		{"extra days", &dto.CreateQuotaTaskRequest{Days: 30}, task.QuotaContent{Days: 30}},
		{"fixed gift", &dto.CreateQuotaTaskRequest{GiftType: 1, GiftValue: 500}, task.QuotaContent{GiftType: 1, GiftValue: 500}},
		{"ratio gift", &dto.CreateQuotaTaskRequest{GiftType: 2, GiftValue: 10, Days: 1}, task.QuotaContent{GiftType: 2, GiftValue: 10, Days: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.req.Subscribers, tc.req.IsActive, tc.req.StartTime, tc.req.EndTime = []int64{3}, new(true), 1700000000000, 1760000000000
			if err := w.svc.CreateQuotaTask(supporttest.Context(), tc.req); err != nil {
				t.Fatal(err)
			}
			row := w.env.ReloadTask(t, 1)
			scope := supporttest.QuotaScope(t, row)
			var content task.QuotaContent
			if err := content.Unmarshal([]byte(row.Content)); err != nil || content != tc.content {
				t.Fatalf("content = %+v (err %v), want %+v", content, err, tc.content)
			}
			if row.Type != int8(task.TypeQuota) || row.Status != task.StatusPending || row.Total != 2 || !reflect.DeepEqual(scope.Objects, []int64{11, 12}) ||
				!reflect.DeepEqual(scope.Subscribers, []int64{3}) || !*scope.IsActive || scope.StartTime != 1700000000000 || scope.EndTime != 1760000000000 ||
				scope.ActorID != supporttest.ActorID {
				t.Fatalf("quota task = %+v, scope %+v", row, scope)
			}
			if selected := w.targets.Queried; len(selected) != 1 || !reflect.DeepEqual(selected[0].Subscribers, []int64{3}) || selected[0].EndTime != 1760000000000 {
				t.Fatalf("selection = %+v", selected)
			}
			if !reflect.DeepEqual(w.queue.Quotas, []int64{1}) {
				t.Fatalf("queued = %v", w.queue.Quotas)
			}
		})
	}
}

func TestQuotaTaskFailures(t *testing.T) {
	down := errors.New("redis down")
	for _, tc := range []struct {
		name       string
		setup      func(t *testing.T, w *world)
		code       uint32
		msg        string
		wantStatus int8 // of the recorded task; -1 when none is recorded
	}{
		{"targets unavailable", func(_ *testing.T, w *world) { w.targets.Err = errors.New("subscription store down") }, xerr.DatabaseQueryError, "", -1},
		{"no targets", func(_ *testing.T, w *world) { w.targets.IDs = nil }, xerr.ERROR, "No subscribers found", -1},
		{"not recorded", func(t *testing.T, w *world) { w.env.Refuse(t, "create", "task", 0, errors.New("disk full")) }, xerr.DatabaseInsertError, "", -1},
		{"not queued", func(_ *testing.T, w *world) { w.queue.Err = down }, xerr.QueueEnqueueError, "", task.StatusEnqueueFailed},
		{"not queued but started", startedWhileQueueFails(down), xerr.QueueEnqueueError, "", task.StatusInProgress},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(t, w)
			refused(t, w.svc.CreateQuotaTask(context.Background(), quota(nil)), tc.code, tc.msg)
			tasks := w.env.AllTasks(t)
			if tc.wantStatus < 0 {
				if len(tasks) != 0 || len(w.queue.Quotas) != 0 {
					t.Fatalf("tasks %+v, queued %v, want nothing", tasks, w.queue.Quotas)
				}
				return
			}
			if len(tasks) != 1 || tasks[0].Status != tc.wantStatus {
				t.Fatalf("tasks = %+v, want one in status %d", tasks, tc.wantStatus)
			}
			if failed := tc.wantStatus == task.StatusEnqueueFailed; failed != (tasks[0].Errors == "enqueue quota task: redis down") {
				t.Fatalf("errors = %q", tasks[0].Errors)
			}
		})
	}
}

// The quota list shows only quota tasks, newest first, narrowed by status;
// no task is an empty list.
func TestQuotaTaskListFiltersAndPages(t *testing.T) {
	w := newWorld(t)
	seedTasks(t, w.env)
	w.env.QuotaTask(t, supporttest.QuotaCampaign{Subscribers: []int64{5}, IsActive: new(false), StartTime: 1, EndTime: 2, Objects: []int64{21, 22},
		ResetTraffic: true, GiftType: 2, GiftValue: 15, Status: task.StatusCompleted, Current: 2, Errors: "none"})
	for _, tc := range []struct {
		name  string
		req   *dto.QueryQuotaTaskListRequest
		total int64
		ids   []int64
	}{
		{"defaults", nil, 2, []int64{4, 3}},
		{"pending", &dto.QueryQuotaTaskListRequest{Status: statusPtr(uint8(task.StatusPending))}, 1, []int64{3}},
		{"second page", &dto.QueryQuotaTaskListRequest{Page: 2, Size: 1}, 2, []int64{3}},
		{"cancelled", &dto.QueryQuotaTaskListRequest{Status: statusPtr(uint8(task.StatusCancelled))}, 0, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := w.svc.QueryQuotaTaskList(context.Background(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			got := []int64{}
			for _, item := range resp.List {
				got = append(got, item.Id)
			}
			if resp.Total != tc.total || resp.List == nil || !reflect.DeepEqual(got, tc.ids) {
				t.Fatalf("list = %+v, want %d %v", resp, tc.total, tc.ids)
			}
		})
	}
	resp, err := w.svc.QueryQuotaTaskList(context.Background(), &dto.QueryQuotaTaskListRequest{Page: 1, Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	row := w.env.ReloadTask(t, 4)
	want := dto.QuotaTask{Id: 4, Subscribers: []int64{5}, IsActive: new(false), StartTime: 1, EndTime: 2, ResetTraffic: true, GiftType: 2, GiftValue: 15,
		Objects: []int64{21, 22}, Status: uint8(task.StatusCompleted), Total: 2, Current: 2, Errors: "none",
		CreatedAt: row.CreatedAt.UnixMilli(), UpdatedAt: row.UpdatedAt.UnixMilli()}
	if len(resp.List) != 1 || !reflect.DeepEqual(resp.List[0], want) {
		t.Fatalf("quota task = %+v, want %+v", resp.List, want)
	}
}

func TestQuotaPreCount(t *testing.T) {
	w := newWorld(t)
	resp, err := w.svc.QueryQuotaTaskPreCount(context.Background(), &dto.QueryQuotaTaskPreCountRequest{Subscribers: []int64{3}, IsActive: new(true), StartTime: 10, EndTime: 20})
	if err != nil || resp.Count != 2 {
		t.Fatalf("count = %+v (err %v), want the two targets", resp, err)
	}
	if counted := w.targets.Counted; len(counted) != 1 || !reflect.DeepEqual(counted[0].Subscribers, []int64{3}) || !*counted[0].IsActive || counted[0].StartTime != 10 || counted[0].EndTime != 20 {
		t.Fatalf("count filter = %+v", counted)
	}
	for _, tc := range []struct {
		req *dto.QueryQuotaTaskPreCountRequest
		msg string
	}{
		{nil, "request is required"},
		{&dto.QueryQuotaTaskPreCountRequest{StartTime: 20, EndTime: 10}, "start_time must not be after end_time"},
		{&dto.QueryQuotaTaskPreCountRequest{StartTime: -1}, "invalid quota task filter"},
		{&dto.QueryQuotaTaskPreCountRequest{Subscribers: []int64{-3}}, "invalid quota task filter"},
	} {
		_, err := w.svc.QueryQuotaTaskPreCount(context.Background(), tc.req)
		refused(t, err, xerr.ERROR, tc.msg)
	}
	w.targets.Err = errors.New("subscription store down")
	_, err = w.svc.QueryQuotaTaskPreCount(context.Background(), &dto.QueryQuotaTaskPreCountRequest{})
	refused(t, err, xerr.DatabaseQueryError, "")
}

// A task list the store cannot read is reported as a query failure.
func TestTaskReadsReportTheStoreFailure(t *testing.T) {
	for _, tc := range []struct {
		name, op, table string
		read            func(s *Service) error
	}{
		{"campaign list", "row", "task", func(s *Service) error {
			_, err := s.GetBatchSendEmailTaskList(context.Background(), nil)
			return err
		}},
		{"campaign list failures", "query", "task_error", func(s *Service) error {
			_, err := s.GetBatchSendEmailTaskList(context.Background(), nil)
			return err
		}},
		{"campaign status failures", "query", "task_error", func(s *Service) error {
			_, err := s.GetBatchSendEmailTaskStatus(context.Background(), &dto.GetBatchSendEmailTaskStatusRequest{Id: 2})
			return err
		}},
		{"quota list", "row", "task", func(s *Service) error {
			_, err := s.QueryQuotaTaskList(context.Background(), nil)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			seedTasks(t, w.env)
			w.env.Refuse(t, tc.op, tc.table, 0, errors.New("connection reset"))
			refused(t, tc.read(w.svc), xerr.DatabaseQueryError, "Database query error")
		})
	}
}
