package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/repository"
	"gorm.io/gorm"
)

// planWriteFixture is the plan, group and client application tables behind
// the real repositories.
type planWriteFixture struct {
	*writeFixture
	plans   repository.SubscribeRepo
	clients repository.ClientRepo
}

func newPlanWriteFixture(t *testing.T) *planWriteFixture {
	t.Helper()
	f := &planWriteFixture{writeFixture: newWriteFixture(t)}
	if err := f.db.AutoMigrate(&subscribe.Group{}, &client.SubscribeApplication{}); err != nil {
		t.Fatal(err)
	}
	conn := repository.ModuleConn{DB: f.db, Redis: f.rds}.Conn()
	f.plans = NewSubscribeRepo(conn, nil)
	f.clients = NewClientRepo(conn)
	return f
}

// created is a creation time an edit's copy never carries; a whole-row save
// would have written the copy's zero time over it.
var created = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

func (f *planWriteFixture) count(t *testing.T, model any) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(model).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// An administrator's edit writes the plan's settings, the ones set to their
// zero value included, and nothing else: the creation time stays, and a plan
// that no longer exists is not re-inserted.
func TestUpdatePlanWritesTheSettingsAndKeepsTheRest(t *testing.T) {
	f := newPlanWriteFixture(t)
	ctx := context.Background()
	on := true
	if err := f.db.Create(&subscribe.Subscribe{Id: 1, Name: "gold", UnitTime: "Month", UnitPrice: 1000, Inventory: 10, Nodes: "1,2", Show: &on, Sell: &on, AllowDeduction: &on, RenewalReset: &on, Sort: 3, CreatedAt: created}).Error; err != nil {
		t.Fatal(err)
	}
	off := false
	edit := &subscribe.Subscribe{Id: 1, Name: "gold+", UnitTime: "Year", UnitPrice: 0, Inventory: 5, Nodes: "", NodeTags: "edge", Show: &on, Sell: &off, AllowDeduction: &on, RenewalReset: &off, Sort: 3}
	if err := f.plans.Update(ctx, edit); err != nil {
		t.Fatal(err)
	}
	var got subscribe.Subscribe
	if err := f.db.First(&got, 1).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "gold+" || got.UnitTime != "Year" || got.UnitPrice != 0 || got.Inventory != 5 || got.Nodes != "" || got.NodeTags != "edge" || *got.Sell || *got.RenewalReset {
		t.Fatalf("plan after the edit: %+v", got)
	}
	if !got.CreatedAt.Equal(created) || !got.UpdatedAt.After(created) {
		t.Fatalf("timestamps after the edit: created %v updated %v", got.CreatedAt, got.UpdatedAt)
	}
	if err := f.plans.Update(ctx, &subscribe.Subscribe{Id: 99, Name: "ghost", UnitTime: "Month", Show: &on, Sell: &on}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, &subscribe.Subscribe{}); n != 1 {
		t.Fatalf("plans = %d; the edit of a missing plan inserted it", n)
	}
	if err := f.plans.Update(ctx, &subscribe.Subscribe{}); err == nil {
		t.Fatal("an edit without a plan id was accepted")
	}
}

// Reordering writes the sort positions only: the plans were read before the
// positions were assigned, and what changed since (a purchase took one unit
// of inventory) must survive; a plan deleted since is not brought back.
func TestUpdateSortWritesOnlyTheSortPositions(t *testing.T) {
	f := newPlanWriteFixture(t)
	ctx := context.Background()
	on := true
	for id := int64(1); id <= 2; id++ {
		if err := f.db.Create(&subscribe.Subscribe{Id: id, Name: "plan", UnitTime: "Month", Inventory: 10, Show: &on, Sell: &on, Sort: id, CreatedAt: created}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var stale []*subscribe.Subscribe
	if err := f.db.Order("id").Find(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&subscribe.Subscribe{}).Where("id = ?", 1).UpdateColumn("inventory", 9).Error; err != nil {
		t.Fatal(err)
	}
	stale[0].Sort, stale[1].Sort = 2, 1
	deleted := &subscribe.Subscribe{Id: 3, Name: "gone", UnitTime: "Month", Show: &on, Sell: &on, Sort: 3}
	if err := f.plans.UpdateSort(ctx, append(stale, deleted)); err != nil {
		t.Fatal(err)
	}
	var got []subscribe.Subscribe
	if err := f.db.Order("id").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Sort != 2 || got[1].Sort != 1 {
		t.Fatalf("plans after the reorder: %+v", got)
	}
	if got[0].Inventory != 9 || !got[0].CreatedAt.Equal(created) {
		t.Fatalf("the reorder wrote the stale copy back: %+v", got[0])
	}
	if !got[0].UpdatedAt.After(created) {
		t.Fatalf("the reorder left the update time: %v", got[0].UpdatedAt)
	}
}

// Group and client application edits write their settings and keep their
// creation time; a group that no longer exists is not re-inserted.
func TestUpdateGroupAndApplicationWriteOnlyTheirSettings(t *testing.T) {
	f := newPlanWriteFixture(t)
	ctx := context.Background()
	if err := f.db.Create(&subscribe.Group{Id: 1, Name: "a", Description: "first", CreatedAt: created}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.plans.UpdateGroup(ctx, &subscribe.Group{Id: 1, Name: "b"}); err != nil {
		t.Fatal(err)
	}
	var group subscribe.Group
	if err := f.db.First(&group, 1).Error; err != nil {
		t.Fatal(err)
	}
	if group.Name != "b" || group.Description != "" || !group.CreatedAt.Equal(created) || !group.UpdatedAt.After(created) {
		t.Fatalf("group after the edit: %+v", group)
	}
	if err := f.plans.UpdateGroup(ctx, &subscribe.Group{Id: 9, Name: "ghost"}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, &subscribe.Group{}); n != 1 {
		t.Fatalf("groups = %d; the edit of a missing group inserted it", n)
	}

	if err := f.db.Create(&client.SubscribeApplication{Id: 1, Name: "Clash", UserAgent: "clash", SubscribeTemplate: "old", OutputFormat: "yaml", DownloadLink: "{}", CreatedAt: created}).Error; err != nil {
		t.Fatal(err)
	}
	app, err := f.clients.FindOne(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	app.Name, app.SubscribeTemplate, app.IsDefault, app.UserAgent = "Clash Meta", "new", true, ""
	if err := f.clients.Update(ctx, app); err != nil {
		t.Fatal(err)
	}
	var got client.SubscribeApplication
	if err := f.db.First(&got, 1).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "Clash Meta" || got.SubscribeTemplate != "new" || !got.IsDefault || got.UserAgent != "" || !got.CreatedAt.Equal(created) || !got.UpdatedAt.After(created) {
		t.Fatalf("application after the edit: %+v", got)
	}
	if err := f.clients.Update(ctx, &client.SubscribeApplication{Id: 9, Name: "ghost"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("the edit of a missing application = %v, want not found", err)
	}
	if n := f.count(t, &client.SubscribeApplication{}); n != 1 {
		t.Fatalf("applications = %d; the edit of a missing application inserted it", n)
	}
}
