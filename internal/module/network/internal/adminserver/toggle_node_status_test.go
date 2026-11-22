package adminserver

import (
	"context"
	"errors"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// A toggle without the switch keeps the stored one (writing NULL failed on
// the NOT NULL column) and one with it applies it; a missing id is a
// parameter error and an unknown node a query error that keeps its cause.
func TestToggleNodeStatusKeepsTheSwitchTheRequestOmits(t *testing.T) {
	db, store := newNodeRepoStore(t)
	server := &node.Server{Name: "edge"}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	disabled := false
	stored := &node.Node{Name: "hk", ServerId: server.Id, Port: 443, Protocol: "vless", Enabled: &disabled}
	if err := db.Create(stored).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(Deps{Store: store})
	enabledAfter := func(want bool) {
		t.Helper()
		var got node.Node
		if err := db.First(&got, stored.Id).Error; err != nil {
			t.Fatal(err)
		}
		if got.Enabled == nil || *got.Enabled != want {
			t.Fatalf("node enabled = %v, want %v", got.Enabled, want)
		}
	}

	if err := svc.ToggleNodeStatus(context.Background(), &dto.ToggleNodeStatusRequest{Id: stored.Id}); err != nil {
		t.Fatalf("ToggleNodeStatus without the switch: %v", err)
	}
	enabledAfter(false)

	enabled := true
	if err := svc.ToggleNodeStatus(context.Background(), &dto.ToggleNodeStatusRequest{Id: stored.Id, Enable: &enabled}); err != nil {
		t.Fatalf("ToggleNodeStatus enabling: %v", err)
	}
	enabledAfter(true)

	err := svc.ToggleNodeStatus(context.Background(), &dto.ToggleNodeStatusRequest{Enable: &enabled})
	if xerr.CodeOf(err) != xerr.InvalidParams {
		t.Fatalf("ToggleNodeStatus without an id = %v, want a parameter error", err)
	}
	err = svc.ToggleNodeStatus(context.Background(), &dto.ToggleNodeStatusRequest{Id: 404, Enable: &enabled})
	if xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("ToggleNodeStatus of an unknown node = %v, want a query error wrapping not found", err)
	}
}
