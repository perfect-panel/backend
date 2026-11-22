package supporttest

import (
	"testing"

	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
)

// EmailCampaign is a batch email task as the marketing service records it.
type EmailCampaign struct {
	Subject, Content       string
	Scope                  int8
	Recipients, Additional []string
	RegisterStartTime      int64
	RegisterEndTime        int64
	Scheduled              int64
	Interval               uint8
	Limit                  uint64
	Status                 int8
	Total, Current         uint64
	Errors                 string
}

// EmailTask stores the campaign and returns its task id.
func (e *Env) EmailTask(t testing.TB, c EmailCampaign) int64 {
	t.Helper()
	scope := task.EmailScope{Type: c.Scope, RegisterStartTime: c.RegisterStartTime, RegisterEndTime: c.RegisterEndTime,
		Recipients: c.Recipients, Additional: c.Additional, Scheduled: c.Scheduled, Interval: c.Interval, Limit: c.Limit}
	scopeJSON, err := scope.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	content := task.EmailContent{Subject: c.Subject, Content: c.Content}
	contentJSON, err := content.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	row := e.Task(t, task.Task{Type: task.TypeEmail, Scope: string(scopeJSON), Content: string(contentJSON),
		Status: c.Status, Errors: c.Errors, Total: c.Total, Current: c.Current})
	return row.Id
}

// QuotaCampaign is a quota gift task as the marketing service records it.
type QuotaCampaign struct {
	Subscribers        []int64
	IsActive           *bool
	StartTime, EndTime int64
	// Objects are the user subscriptions the task applies to.
	Objects      []int64
	ResetTraffic bool
	Days         uint64
	GiftType     uint8
	GiftValue    uint64
	Status       int8
	Current      uint64
	Errors       string
}

// QuotaTask stores the quota task and returns its task id; its total is the
// number of its objects.
func (e *Env) QuotaTask(t testing.TB, q QuotaCampaign) int64 {
	t.Helper()
	scope := task.QuotaScope{Subscribers: q.Subscribers, IsActive: q.IsActive, StartTime: q.StartTime, EndTime: q.EndTime, Objects: q.Objects}
	scopeJSON, err := scope.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	content := task.QuotaContent{ResetTraffic: q.ResetTraffic, Days: q.Days, GiftType: q.GiftType, GiftValue: q.GiftValue}
	contentJSON, err := content.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	row := e.Task(t, task.Task{Type: task.TypeQuota, Scope: string(scopeJSON), Content: string(contentJSON),
		Status: q.Status, Errors: q.Errors, Total: uint64(len(q.Objects)), Current: q.Current})
	return row.Id
}

// TaskFailure stores the failure of the target at position of a task, as a
// worker records it.
func (e *Env) TaskFailure(t testing.TB, taskID int64, position uint64, target, reason string, at int64) {
	t.Helper()
	e.TaskError(t, task.TaskError{TaskId: taskID, Position: position, Target: target, Error: reason, OccurredAt: at})
}

// EmailScope decodes the scope a stored email task records.
func EmailScope(t testing.TB, row task.Task) task.EmailScope {
	t.Helper()
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(row.Scope)); err != nil {
		t.Fatalf("scope of task %d: %v", row.Id, err)
	}
	return scope
}

// QuotaScope decodes the scope a stored quota task records.
func QuotaScope(t testing.TB, row task.Task) task.QuotaScope {
	t.Helper()
	var scope task.QuotaScope
	if err := scope.Unmarshal([]byte(row.Scope)); err != nil {
		t.Fatalf("scope of task %d: %v", row.Id, err)
	}
	return scope
}
