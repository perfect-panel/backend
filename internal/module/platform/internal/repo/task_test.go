package repo

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/module/platform/entity/task"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskRepoActiveUpdatesAreTypeAndStateGuarded(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-state-guards?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&task.Task{}); err != nil {
		t.Fatal(err)
	}
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
}
