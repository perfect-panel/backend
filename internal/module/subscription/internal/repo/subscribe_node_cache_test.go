package repo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
)

// recordingBridge records the node scopes the plan writes hand the network
// bundle's fenced invalidation.
type recordingBridge struct {
	calls []string
	err   error
}

func (b *recordingBridge) ClearNodeUserListCaches(_ context.Context, nodeIDs []int64, tags []string) error {
	ids := slices.Clone(nodeIDs)
	slices.Sort(ids)
	tags = slices.Clone(tags)
	slices.Sort(tags)
	b.calls = append(b.calls, fmt.Sprintf("nodes=%v tags=%v", ids, tags))
	return b.err
}

// Plan writes invalidate the node-facing server caches through the network
// bundle, after the write, instead of deleting the `server:user:*` keys
// themselves: the plans' own keys carry no node keys, and the bridge sees
// the scope of every write that changes what the servers serve.
func TestPlanWritesClearNodeCachesThroughTheFencedBridge(t *testing.T) {
	f := newWriteFixture(t)
	bridge := &recordingBridge{}
	plans := NewSubscribeRepo(cache.NewConn(f.db, f.rds), bridge).(*subscribeRepo)
	ctx := context.Background()

	plan := &subscribe.Subscribe{Name: "plan", Nodes: "9,10", NodeTags: "edge,core", Inventory: 5}
	if err := plans.Insert(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if len(bridge.calls) != 0 {
		t.Fatalf("a new plan, which serves nobody yet, cleared node caches: %v", bridge.calls)
	}
	for _, key := range plans.cacheKeys(plan) {
		if strings.HasPrefix(key, "server:user:") {
			t.Fatalf("cacheKeys still names a node key to delete: %q", key)
		}
	}

	if err := plans.ClearCache(ctx, plan.Id); err != nil {
		t.Fatal(err)
	}
	want := []string{"nodes=[9 10] tags=[core edge]"}
	if !slices.Equal(bridge.calls, want) {
		t.Fatalf("ClearCache cleared %v, want %v", bridge.calls, want)
	}

	// Inventory changes what is sold, not what the servers serve.
	bridge.calls = nil
	if reserved, err := plans.ReserveInventory(ctx, plan.Id); err != nil || reserved != repository.InventoryDeducted {
		t.Fatalf("ReserveInventory = %v, %v", reserved, err)
	}
	if err := plans.RestoreInventory(ctx, plan.Id); err != nil {
		t.Fatal(err)
	}
	if len(bridge.calls) != 0 {
		t.Fatalf("inventory writes cleared node caches: %v", bridge.calls)
	}

	// A moved plan clears the servers of the previous and of the new scope.
	moved := *plan
	moved.Nodes = "11"
	moved.NodeTags = ""
	if err := plans.Update(ctx, &moved); err != nil {
		t.Fatal(err)
	}
	want = []string{"nodes=[9 10 11] tags=[core edge]"}
	if !slices.Equal(bridge.calls, want) {
		t.Fatalf("Update cleared %v, want %v", bridge.calls, want)
	}

	// A bridge failure after a committed write is logged, not returned;
	// ClearCache, the explicit invalidation, reports it.
	bridge.calls, bridge.err = nil, errors.New("redis unavailable")
	if err := plans.Update(ctx, &moved); err != nil {
		t.Fatalf("Update returned the cache failure of a committed write: %v", err)
	}
	if err := plans.ClearCache(ctx, plan.Id); !errors.Is(err, bridge.err) {
		t.Fatalf("ClearCache = %v, want the bridge's error", err)
	}

	bridge.calls, bridge.err = nil, nil
	if err := plans.Delete(ctx, plan.Id); err != nil {
		t.Fatal(err)
	}
	want = []string{"nodes=[11] tags=[]"}
	if !slices.Equal(bridge.calls, want) {
		t.Fatalf("Delete cleared %v, want %v", bridge.calls, want)
	}

	// Without a bridge the plan writes still work.
	bare := NewSubscribeRepo(cache.NewConn(f.db, f.rds), nil)
	if err := bare.ClearCache(ctx, 404); err != nil {
		t.Fatal(err)
	}
}
