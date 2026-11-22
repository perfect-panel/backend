package repo

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// An administrator's server update writes the settings columns, zero values
// included, and leaves the nodes' heartbeat alone: the whole-row save wrote
// the stale last_reported_at of the loaded row back over a heartbeat that
// landed meanwhile.
func TestUpdateServerWritesOnlyTheAdminColumns(t *testing.T) {
	db, _, repo := newNodeRepoWithCache(t)
	ctx := context.Background()
	reported := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	server := &node.Server{Name: "edge", Country: "DE", City: "Berlin", Address: "1.2.3.4", LastReportedAt: &reported}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.FindOneServer(ctx, server.Id)
	if err != nil {
		t.Fatal(err)
	}

	// A heartbeat lands after the admin loaded the row.
	heartbeat := reported.Add(time.Hour)
	if err := db.Model(&node.Server{}).Where("id = ?", server.Id).UpdateColumn("last_reported_at", heartbeat).Error; err != nil {
		t.Fatal(err)
	}
	loaded.Name = "edge-2"
	loaded.Country = ""
	loaded.Protocols = `[{"type":"vless","port":443}]`
	before := time.Now()
	if err := repo.UpdateServer(ctx, loaded); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}

	got, err := repo.FindOneServer(ctx, server.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "edge-2" || got.Country != "" || got.Protocols != loaded.Protocols || got.City != "Berlin" {
		t.Fatalf("settings after the update = %+v", got)
	}
	if got.LastReportedAt == nil || !got.LastReportedAt.Equal(heartbeat) {
		t.Fatalf("last_reported_at = %v, want the heartbeat %v kept", got.LastReportedAt, heartbeat)
	}
	if got.UpdatedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("updated_at = %v was not advanced", got.UpdatedAt)
	}
	if !got.CreatedAt.Equal(server.CreatedAt) {
		t.Fatalf("created_at changed from %v to %v", server.CreatedAt, got.CreatedAt)
	}
}

// A node update writes its settings columns, zero values (a cleared tag list,
// a disabled switch) included.
func TestUpdateNodeWritesZeroValuesOfTheAdminColumns(t *testing.T) {
	db, _, repo := newNodeRepoWithCache(t)
	ctx := context.Background()
	server := &node.Server{Name: "edge"}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	enabled := true
	item := &node.Node{Name: "hk", ServerId: server.Id, Port: 443, Address: "hk.example", Protocol: "vless", Tags: "edge,premium", Enabled: &enabled}
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.FindOneNode(ctx, item.Id)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	loaded.Tags = ""
	loaded.Enabled = &disabled
	loaded.Port = 0
	loaded.Server = &node.Server{Id: server.Id, Name: "must not be saved"}
	if err := repo.UpdateNode(ctx, loaded); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	got, err := repo.FindOneNode(ctx, item.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tags != "" || got.Enabled == nil || *got.Enabled || got.Port != 0 || got.Name != "hk" {
		t.Fatalf("node after the update = %+v", got)
	}
	var owner node.Server
	if err := db.First(&owner, server.Id).Error; err != nil || owner.Name != "edge" {
		t.Fatalf("the loaded association was written: %+v, %v", owner, err)
	}
}

// The configuration override is created once and then updated in place; a
// nil value is written as NULL, which means inherit.
func TestSaveServerConfigOverrideUpdatesInPlace(t *testing.T) {
	_, _, repo := newNodeRepoWithCache(t)
	ctx := context.Background()
	dns, block := "8.8.8.8", "ads"
	first := &node.ServerConfigOverride{ServerId: 7, DNS: &dns, Block: &block}
	if err := repo.SaveServerConfigOverride(ctx, first); err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.Id == 0 {
		t.Fatal("the created override has no id")
	}
	outbound := "direct"
	second := &node.ServerConfigOverride{ServerId: 7, Block: &block, Outbound: &outbound}
	if err := repo.SaveServerConfigOverride(ctx, second); err != nil {
		t.Fatalf("update: %v", err)
	}
	if second.Id != first.Id {
		t.Fatalf("the update created a second row: %d != %d", second.Id, first.Id)
	}
	got, err := repo.FindServerConfigOverride(ctx, 7)
	if err != nil || got == nil {
		t.Fatalf("FindServerConfigOverride = %v, %v", got, err)
	}
	if got.DNS != nil || got.Block == nil || *got.Block != block || got.Outbound == nil || *got.Outbound != outbound {
		t.Fatalf("override after the update = %+v", got)
	}
	if !got.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at changed from %v to %v", first.CreatedAt, got.CreatedAt)
	}
}
