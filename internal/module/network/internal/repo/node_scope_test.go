package repo

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListNodesByScopeUsesORSemanticsAndPreloadsServer(t *testing.T) {
	// A shared-cache in-memory database lives as long as one connection to
	// it is open; closing the pool discards it, so a repeated run (-count)
	// starts from an empty one instead of colliding on the unique sort.
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&node.Server{}, &node.Node{}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	server := &node.Server{Name: "server"}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	nodes := []*node.Node{
		{Name: "direct", ServerId: server.Id, Tags: "other", Enabled: &enabled, Sort: 1},
		{Name: "tagged", ServerId: server.Id, Tags: "edge,premium", Enabled: &enabled, Sort: 2},
		{Name: "unmatched", ServerId: server.Id, Tags: "other", Enabled: &enabled, Sort: 3},
	}
	if err := db.Create(nodes).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewNodeRepo(db, nil)
	matched, err := repo.ListNodesByScope(context.Background(), []int64{nodes[0].Id}, []string{"edge"}, &enabled, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 2 || matched[0].Name != "direct" || matched[1].Name != "tagged" {
		t.Fatalf("matched nodes = %+v", matched)
	}
	if matched[0].Server == nil || matched[1].Server == nil {
		t.Fatalf("servers were not preloaded: %+v", matched)
	}
}
