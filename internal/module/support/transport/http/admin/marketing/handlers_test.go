package marketing

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The marketing handlers run against the real support facade over the
// harness database. Each case starts from a campaign half sent (task 1) with
// one failed recipient, and a pending quota task (task 2); the recipient
// port selects a@example.com and b@example.com, the target port the user
// subscriptions 11 and 12.

type marketingWorld struct {
	env        *supporttest.Env
	h          *server.Hertz
	recipients *supporttest.Recipients
	targets    *supporttest.QuotaTargets
	queue      *supporttest.Queue
	stopper    *supporttest.Stopper
}

func marketingFixture(t *testing.T) marketingWorld {
	t.Helper()
	w := marketingWorld{
		env:        supporttest.New(t),
		recipients: &supporttest.Recipients{Emails: []string{"a@example.com", "b@example.com"}},
		targets:    &supporttest.QuotaTargets{IDs: []int64{11, 12}},
		queue:      &supporttest.Queue{},
		stopper:    &supporttest.Stopper{},
	}
	w.env.EmailTask(t, supporttest.EmailCampaign{Subject: "welcome", Content: "<p>hi</p>", Scope: 1,
		Recipients: []string{"a@example.com", "b@example.com"}, Scheduled: 1758000000, Status: supporttest.TaskInProgress, Total: 2, Current: 1})
	w.env.TaskFailure(t, 1, 0, "a@example.com", "mailbox full", 1758000100)
	w.env.QuotaTask(t, supporttest.QuotaCampaign{Subscribers: []int64{3}, IsActive: new(true), Objects: []int64{11, 12}, Days: 7})
	svc := support.New(support.Deps{Tasks: w.env.Tasks, Recipients: w.recipients, QuotaTargets: w.targets, Queue: w.queue, EmailStopper: w.stopper})

	w.h = server.New()
	// The access-log and auth middlewares put the administrator's request
	// metadata into the context; the tasks record it.
	w.h.Use(func(ctx context.Context, c *app.RequestContext) { c.Next(supporttest.WithMetadata(ctx)) })
	group := w.h.Group("/v1/admin/marketing")
	group.GET("/email/batch/list", GetBatchSendEmailTaskListHandler(svc))
	group.POST("/email/batch/pre-send-count", GetPreSendEmailCountHandler(svc))
	group.POST("/email/batch/send", CreateBatchSendEmailTaskHandler(svc))
	group.POST("/email/batch/status", GetBatchSendEmailTaskStatusHandler(svc))
	group.POST("/email/batch/stop", StopBatchSendEmailTaskHandler(svc))
	group.POST("/quota/create", CreateQuotaTaskHandler(svc))
	group.GET("/quota/list", QueryQuotaTaskListHandler(svc))
	group.POST("/quota/pre-count", QueryQuotaTaskPreCountHandler(svc))
	return w
}

// campaignView is how the admin API lists the fixture's campaign.
func campaignView(w marketingWorld, t *testing.T) dto.BatchSendEmailTask {
	row := w.env.ReloadTask(t, 1)
	return dto.BatchSendEmailTask{Id: 1, Subject: "welcome", Content: "<p>hi</p>", Recipients: "a@example.com\nb@example.com", RecipientCount: 2, Scope: 1,
		Scheduled: 1758000000, Status: uint8(supporttest.TaskInProgress), Total: 2, Current: 1,
		Errors:    `[{"error":"mailbox full","email":"a@example.com","time":1758000100}]`,
		CreatedAt: row.CreatedAt.UnixMilli(), UpdatedAt: row.UpdatedAt.UnixMilli()}
}

// quotaView is how the admin API lists the fixture's quota task.
func quotaView(w marketingWorld, t *testing.T) dto.QuotaTask {
	row := w.env.ReloadTask(t, 2)
	return dto.QuotaTask{Id: 2, Subscribers: []int64{3}, IsActive: new(true), Days: 7, Objects: []int64{11, 12}, Total: 2,
		Status: uint8(supporttest.TaskPending), CreatedAt: row.CreatedAt.UnixMilli(), UpdatedAt: row.UpdatedAt.UnixMilli()}
}

func TestMarketingHandlersRunTheCampaigns(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		queueDown                  bool
		check                      func(t *testing.T, w marketingWorld, reply supporttest.Reply)
	}{
		// A campaign sends to the selected recipients and to the additional
		// addresses once each, and starts ten seconds after it is created.
		{"send", http.MethodPost, "/v1/admin/marketing/email/batch/send",
			`{"subject":"news","content":"<p>v2</p>","scope":2,"register_start_time":1700000000,"register_end_time":1760000000,` +
				`"additional":"C@example.com\nb@example.com","interval":5,"limit":100}`, false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				row := w.env.ReloadTask(t, 3)
				scope := supporttest.EmailScope(t, row)
				if row.Status != supporttest.TaskPending || row.Total != 3 || scope.Type != 2 || scope.Interval != 5 || scope.Limit != 100 ||
					!reflect.DeepEqual(scope.Recipients, []string{"a@example.com", "b@example.com"}) ||
					!reflect.DeepEqual(scope.Additional, []string{"c@example.com", "b@example.com"}) {
					t.Fatalf("campaign = %+v, scope %+v", row, scope)
				}
				if scope.ClientIP != supporttest.ClientIP || scope.UserAgent != supporttest.UserAgent || scope.ActorID != supporttest.ActorID {
					t.Fatalf("campaign request metadata = %+v, want the administrator's request", scope.Metadata)
				}
				if len(w.recipients.Queried) != 1 || w.recipients.Queried[0].Scope != 2 ||
					w.recipients.Queried[0].RegisterStartTime != 1700000000 || w.recipients.Queried[0].RegisterEndTime != 1760000000 {
					t.Fatalf("recipient selection = %+v", w.recipients.Queried)
				}
				if len(w.queue.Emails) != 1 || w.queue.Emails[0].TaskID != 3 || time.Until(w.queue.Emails[0].ProcessAt) < 5*time.Second {
					t.Fatalf("queued = %+v, want the campaign to run in ten seconds", w.queue.Emails)
				}
			}},
		// The queue refused the campaign: the admin sees the enqueue error,
		// and the campaign is marked so rather than left pending.
		{"send not queued", http.MethodPost, "/v1/admin/marketing/email/batch/send", `{"subject":"news","content":"<p>v2</p>","scope":1}`, true,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.QueueEnqueueError, "Queue enqueue error")
				if row := w.env.ReloadTask(t, 3); row.Status != supporttest.TaskEnqueueFailed || row.Errors == "" {
					t.Fatalf("campaign = %+v, want it marked enqueue-failed", row)
				}
			}},
		{"pre-send count", http.MethodPost, "/v1/admin/marketing/email/batch/pre-send-count", `{"scope":3,"register_start_time":1700000000}`, false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, `{"count":2}`)
				if len(w.recipients.Counted) != 1 || w.recipients.Counted[0].Scope != 3 || w.recipients.Counted[0].RegisterStartTime != 1700000000 {
					t.Fatalf("recipient count = %+v", w.recipients.Counted)
				}
			}},
		{"pre-send count with additional addresses", http.MethodPost, "/v1/admin/marketing/email/batch/pre-send-count",
			`{"scope":1,"additional":"a@example.com\nz@example.com"}`, false,
			func(t *testing.T, _ marketingWorld, reply supporttest.Reply) {
				reply.OK(t, `{"count":3}`)
			}},
		{"campaign list", http.MethodGet, "/v1/admin/marketing/email/batch/list?page=1&size=10&scope=1&status=1", "", false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetBatchSendEmailTaskListResponse{Total: 1, List: []dto.BatchSendEmailTask{campaignView(w, t)}})
			}},
		{"campaign list without match", http.MethodGet, "/v1/admin/marketing/email/batch/list?status=4", "", false,
			func(t *testing.T, _ marketingWorld, reply supporttest.Reply) {
				reply.OK(t, `{"total":0,"list":[]}`)
			}},
		{"campaign status", http.MethodPost, "/v1/admin/marketing/email/batch/status", `{"id":1}`, false,
			func(t *testing.T, _ marketingWorld, reply supporttest.Reply) {
				reply.OK(t, `{"status":1,"current":1,"total":2,"errors":"[{\"error\":\"mailbox full\",\"email\":\"a@example.com\",\"time\":1758000100}]"}`)
			}},
		// A quota task is not a campaign.
		{"campaign status of a quota task", http.MethodPost, "/v1/admin/marketing/email/batch/status", `{"id":2}`, false,
			func(t *testing.T, _ marketingWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
			}},
		{"stop", http.MethodPost, "/v1/admin/marketing/email/batch/stop", `{"id":1}`, false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				if row := w.env.ReloadTask(t, 1); row.Status != supporttest.TaskCancelled || !reflect.DeepEqual(w.stopper.Stopped, []int64{1}) {
					t.Fatalf("campaign = %+v, stopped = %v, want it cancelled and its worker stopped", row, w.stopper.Stopped)
				}
			}},
		{"stop a quota task", http.MethodPost, "/v1/admin/marketing/email/batch/stop", `{"id":2}`, false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.ERROR, "email task is not stoppable")
				if row := w.env.ReloadTask(t, 2); row.Status != supporttest.TaskPending || len(w.stopper.Stopped) != 0 {
					t.Fatalf("quota task = %+v, stopped = %v, want it untouched", row, w.stopper.Stopped)
				}
			}},
		{"quota create", http.MethodPost, "/v1/admin/marketing/quota/create",
			`{"subscribers":[3],"is_active":true,"start_time":1700000000000,"end_time":1760000000000,"reset_traffic":true,"days":30,"gift_type":1,"gift_value":500}`, false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				row := w.env.ReloadTask(t, 3)
				scope := supporttest.QuotaScope(t, row)
				if row.Status != supporttest.TaskPending || row.Total != 2 || !reflect.DeepEqual(scope.Objects, []int64{11, 12}) ||
					!reflect.DeepEqual(scope.Subscribers, []int64{3}) || scope.IsActive == nil || !*scope.IsActive || scope.ActorID != supporttest.ActorID {
					t.Fatalf("quota task = %+v, scope %+v", row, scope)
				}
				selected := w.targets.Queried
				if len(selected) != 1 || !reflect.DeepEqual(selected[0].Subscribers, []int64{3}) || selected[0].StartTime != 1700000000000 || selected[0].EndTime != 1760000000000 {
					t.Fatalf("target selection = %+v", selected)
				}
				if !reflect.DeepEqual(w.queue.Quotas, []int64{3}) {
					t.Fatalf("queued = %v, want the quota task", w.queue.Quotas)
				}
			}},
		{"quota list", http.MethodGet, "/v1/admin/marketing/quota/list?page=1&size=10&status=0", "", false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, dto.QueryQuotaTaskListResponse{Total: 1, List: []dto.QuotaTask{quotaView(w, t)}})
			}},
		// No task matches: the list is empty, not null.
		{"quota list without match", http.MethodGet, "/v1/admin/marketing/quota/list?status=4", "", false,
			func(t *testing.T, _ marketingWorld, reply supporttest.Reply) {
				reply.OK(t, `{"total":0,"list":[]}`)
			}},
		{"quota pre-count", http.MethodPost, "/v1/admin/marketing/quota/pre-count", `{"subscribers":[3],"is_active":false,"start_time":1700000000000}`, false,
			func(t *testing.T, w marketingWorld, reply supporttest.Reply) {
				reply.OK(t, `{"count":2}`)
				if counted := w.targets.Counted; len(counted) != 1 || counted[0].IsActive == nil || *counted[0].IsActive || counted[0].StartTime != 1700000000000 {
					t.Fatalf("target count = %+v", counted)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := marketingFixture(t)
			if tc.queueDown {
				w.queue.Err = errors.New("redis down")
			}
			tc.check(t, w, supporttest.Serve(t, w.h, tc.method, tc.target, tc.body))
		})
	}
}

// A request that does not bind or fails validation is refused as a
// parameter error: no task is recorded, queued or stopped.
func TestMarketingHandlersRefuseMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		msg                        string
	}{
		{"send not JSON", http.MethodPost, "/v1/admin/marketing/email/batch/send", `{"subject":"news"`, ""},
		{"send without subject", http.MethodPost, "/v1/admin/marketing/email/batch/send", `{"content":"<p>v2</p>","scope":1}`, "Subject is a required field"},
		{"send to an unknown scope", http.MethodPost, "/v1/admin/marketing/email/batch/send", `{"subject":"news","content":"<p>v2</p>","scope":9}`, "Scope must be one of"},
		{"pre-send count not JSON", http.MethodPost, "/v1/admin/marketing/email/batch/pre-send-count", `{"scope":`, ""},
		{"pre-send count without scope", http.MethodPost, "/v1/admin/marketing/email/batch/pre-send-count", `{}`, "Scope is a required field"},
		{"campaign list page not a number", http.MethodGet, "/v1/admin/marketing/email/batch/list?page=first", "", "bind Page"},
		{"campaign list unknown scope", http.MethodGet, "/v1/admin/marketing/email/batch/list?scope=9", "", "Scope must be one of"},
		{"campaign status not JSON", http.MethodPost, "/v1/admin/marketing/email/batch/status", `{"id":}`, ""},
		{"campaign status without id", http.MethodPost, "/v1/admin/marketing/email/batch/status", `{}`, "Id is a required field"},
		{"stop id not a number", http.MethodPost, "/v1/admin/marketing/email/batch/stop", `{"id":"1"}`, ""},
		{"stop negative id", http.MethodPost, "/v1/admin/marketing/email/batch/stop", `{"id":-1}`, "Id must be greater than 0"},
		{"quota create not JSON", http.MethodPost, "/v1/admin/marketing/quota/create", `{"days":7,}`, ""},
		{"quota create unknown gift type", http.MethodPost, "/v1/admin/marketing/quota/create", `{"days":7,"gift_type":3,"gift_value":1}`, "GiftType must be one of"},
		{"quota create invalid subscriber", http.MethodPost, "/v1/admin/marketing/quota/create", `{"subscribers":[0],"days":7}`, "Subscribers[0] must be greater than 0"},
		{"quota list size not a number", http.MethodGet, "/v1/admin/marketing/quota/list?size=all", "", "bind Size"},
		{"quota list unknown status", http.MethodGet, "/v1/admin/marketing/quota/list?status=9", "", "Status must be one of"},
		{"quota pre-count not JSON", http.MethodPost, "/v1/admin/marketing/quota/pre-count", `[3]`, ""},
		{"quota pre-count negative start", http.MethodPost, "/v1/admin/marketing/quota/pre-count", `{"start_time":-1}`, "StartTime must be 0 or greater"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := marketingFixture(t)
			supporttest.Serve(t, w.h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidParams, tc.msg)
			if n := len(w.env.AllTasks(t)); n != 2 || len(w.queue.Emails)+len(w.queue.Quotas)+len(w.stopper.Stopped) != 0 {
				t.Fatalf("%d tasks, queued %+v %+v, stopped %v, want nothing recorded", n, w.queue.Emails, w.queue.Quotas, w.stopper.Stopped)
			}
			if len(w.recipients.Queried)+len(w.recipients.Counted)+len(w.targets.Queried)+len(w.targets.Counted) != 0 {
				t.Fatal("a refused request selected recipients or targets")
			}
		})
	}
}
