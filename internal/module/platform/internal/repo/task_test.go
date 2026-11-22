package repo

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestMySQLTaskScopeFilterUsesGeneratedColumnIndexShape(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=true",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry-run mysql: %v", err)
	}

	scope := task.ScopeActive.Int8()
	var rows []*task.Task
	stmt := db.Model(&task.Task{}).
		Where("scope_type = ?", scope).
		Select(taskPageSelect).
		Order("created_at DESC, id DESC").
		Scan(&rows).Statement
	sql := stmt.SQL.String()
	for _, want := range []string{"scope_type = ?", "COUNT(*) OVER()", "ORDER BY created_at DESC, id DESC"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("task query missing %q:\n%s", want, sql)
		}
	}
}

func TestTaskRepoActiveUpdatesAreTypeAndStateGuarded(t *testing.T) {
	db := openTestDB(t, "task-state-guards", &task.Task{}, &task.TaskError{})
	repo := NewTaskRepo(db)
	data := &task.Task{Type: task.TypeEmail, Status: task.StatusPending, Scope: `{}`, Content: `{}`}
	if err := repo.Insert(context.Background(), data); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateStatusFrom(context.Background(), data.Id, task.TypeQuota, []int8{task.StatusPending}, task.StatusCancelled)
	if err != nil || updated {
		t.Fatalf("wrong task type updated: updated=%v err=%v", updated, err)
	}
	updated, err = repo.UpdateStatusFrom(context.Background(), data.Id, task.TypeEmail, []int8{task.StatusPending}, task.StatusCancelled)
	if err != nil || !updated {
		t.Fatalf("active email task was not cancelled: updated=%v err=%v", updated, err)
	}

	data.Status = task.StatusCompleted
	updated, err = repo.UpdateActive(context.Background(), data)
	if err != nil || updated {
		t.Fatalf("terminal task accepted stale worker update: updated=%v err=%v", updated, err)
	}
	stored, err := repo.FindOneByType(context.Background(), data.Id, task.TypeEmail)
	if err != nil || stored.Status != task.StatusCancelled {
		t.Fatalf("cancelled status was overwritten: task=%+v err=%v", stored, err)
	}
	updated, err = repo.UpdateStatusAndErrorFrom(context.Background(), data.Id, task.TypeEmail, []int8{task.StatusPending}, task.StatusEnqueueFailed, "redis unavailable")
	if err != nil || updated {
		t.Fatalf("terminal task accepted enqueue-failure overwrite: updated=%v err=%v", updated, err)
	}

	progress := &task.Task{Id: data.Id, Type: task.TypeEmail, Status: task.StatusInProgress, Current: 1, Total: 2, DailyDate: "2026-08-28", DailySent: 1}
	// Re-open the task only for verifying the bounded progress update shape.
	if err := db.Model(&task.Task{}).Where("id = ?", data.Id).Update("status", task.StatusPending).Error; err != nil {
		t.Fatal(err)
	}
	updated, err = repo.UpdateActiveProgress(context.Background(), progress)
	if err != nil || !updated {
		t.Fatalf("progress update failed: updated=%v err=%v", updated, err)
	}
	stored, err = repo.FindOneByType(context.Background(), data.Id, task.TypeEmail)
	if err != nil || stored.Scope != `{}` || stored.Content != `{}` || stored.Current != 1 || stored.DailySent != 1 {
		t.Fatalf("progress update rewrote immutable payload or missed counters: task=%+v err=%v", stored, err)
	}

	quota := &task.Task{Type: task.TypeQuota, Status: task.StatusPending, Scope: `{}`, Content: `{}`}
	if err := repo.Insert(context.Background(), quota); err != nil {
		t.Fatal(err)
	}
	updated, err = repo.UpdateStatusAndErrorFrom(context.Background(), quota.Id, task.TypeQuota, []int8{task.StatusPending}, task.StatusEnqueueFailed, "redis unavailable")
	if err != nil || !updated {
		t.Fatalf("pending task did not record enqueue failure: updated=%v err=%v", updated, err)
	}
	stored, err = repo.FindOneByType(context.Background(), quota.Id, task.TypeQuota)
	if err != nil || stored.Status != task.StatusEnqueueFailed || stored.Errors != "redis unavailable" {
		t.Fatalf("enqueue failure was not recorded atomically: task=%+v err=%v", stored, err)
	}

	taskError := &task.TaskError{TaskId: data.Id, Position: 3, Target: "failed@example.com", Error: "rejected", OccurredAt: 1}
	if err := repo.InsertError(context.Background(), taskError); err != nil {
		t.Fatal(err)
	}
	// Queue retries can observe the same failed position; the unique key keeps
	// the incremental failure log idempotent.
	if err := repo.InsertError(context.Background(), taskError); err != nil {
		t.Fatal(err)
	}
	errors, err := repo.FindErrors(context.Background(), []int64{data.Id})
	if err != nil || len(errors) != 1 || errors[0].Target != "failed@example.com" {
		t.Fatalf("task errors = %+v err=%v", errors, err)
	}

	activeScope, _ := (&task.EmailScope{Type: task.ScopeActive.Int8()}).Marshal()
	expiredScope, _ := (&task.EmailScope{Type: task.ScopeExpired.Int8()}).Marshal()
	if err := repo.Insert(context.Background(), &task.Task{Type: task.TypeEmail, Status: task.StatusPending, Scope: string(activeScope), Content: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(context.Background(), &task.Task{Type: task.TypeEmail, Status: task.StatusPending, Scope: string(expiredScope), Content: `{}`}); err != nil {
		t.Fatal(err)
	}
	scopeFilter := task.ScopeActive.Int8()
	total, filtered, err := repo.QueryTaskList(context.Background(), &task.Filter{Type: task.TypeEmail, Page: 1, Size: 10, Scope: &scopeFilter})
	if err != nil || total != 1 || len(filtered) != 1 {
		t.Fatalf("database-side scope filter total=%d list=%d err=%v", total, len(filtered), err)
	}
	total, filtered, err = repo.QueryTaskList(context.Background(), &task.Filter{Type: task.TypeEmail, Page: 2, Size: 10, Scope: &scopeFilter})
	if err != nil || total != 1 || len(filtered) != 0 {
		t.Fatalf("out-of-range scope page total=%d list=%d err=%v", total, len(filtered), err)
	}
}
