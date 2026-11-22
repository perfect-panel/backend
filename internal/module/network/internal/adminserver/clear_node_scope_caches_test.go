package adminserver

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// scopeCaches resolves every scope to its nodes and records the lookups and
// the cleared servers.
type scopeCaches struct {
	nodes    []*node.Node
	listErr  error
	clearErr map[int64]error
	lookups  []scopeLookup
	cleared  []int64
}

type scopeLookup struct {
	nodeIDs []int64
	tags    []string
	enabled *bool
	preload bool
}

var _ scopeCacheStore = (*scopeCaches)(nil)

func (s *scopeCaches) ListNodesByScope(_ context.Context, nodeIDs []int64, tags []string, enabled *bool, preload bool) ([]*node.Node, error) {
	s.lookups = append(s.lookups, scopeLookup{nodeIDs: nodeIDs, tags: tags, enabled: enabled, preload: preload})
	return s.nodes, s.listErr
}

func (s *scopeCaches) ClearServerCache(_ context.Context, serverID int64) error {
	s.cleared = append(s.cleared, serverID)
	return s.clearErr[serverID]
}

// One lookup resolves the scope's nodes, disabled ones included, and every
// server carrying them is cleared once.
func TestClearServerCachesByNodeScopeClearsEachServerOnce(t *testing.T) {
	store := &scopeCaches{nodes: []*node.Node{{Id: 1, ServerId: 3}, {Id: 2, ServerId: 1}, {Id: 3, ServerId: 3}, {Id: 4}, nil}}
	if err := clearServerCachesByNodeScope(context.Background(), store, []int64{1, 2}, []string{"hk", ""}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if want := []scopeLookup{{nodeIDs: []int64{1, 2}, tags: []string{"hk"}}}; !reflect.DeepEqual(store.lookups, want) {
		t.Fatalf("lookups = %+v, want %+v", store.lookups, want)
	}
	if want := []int64{1, 3}; !reflect.DeepEqual(store.cleared, want) {
		t.Fatalf("cleared servers = %v, want %v", store.cleared, want)
	}
}

// An empty scope would select every node: nothing is looked up or cleared.
func TestClearServerCachesByNodeScopeSkipsAnEmptyScope(t *testing.T) {
	store := &scopeCaches{nodes: []*node.Node{{Id: 1, ServerId: 3}}}
	if err := clearServerCachesByNodeScope(context.Background(), store, nil, []string{""}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(store.lookups) != 0 || len(store.cleared) != 0 {
		t.Fatalf("empty scope looked up %+v and cleared %v", store.lookups, store.cleared)
	}
}

// A failed lookup is reported; a failed server does not stop the others.
func TestClearServerCachesByNodeScopeReportsFailures(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	store := &scopeCaches{listErr: lookupErr}
	if err := clearServerCachesByNodeScope(context.Background(), store, []int64{1}, nil); !errors.Is(err, lookupErr) {
		t.Fatalf("lookup failure = %v", err)
	}

	clearErr := errors.New("redis unavailable")
	store = &scopeCaches{
		nodes:    []*node.Node{{Id: 1, ServerId: 1}, {Id: 2, ServerId: 2}},
		clearErr: map[int64]error{1: clearErr},
	}
	if err := clearServerCachesByNodeScope(context.Background(), store, nil, []string{"hk"}); !errors.Is(err, clearErr) {
		t.Fatalf("clear failure = %v", err)
	}
	if want := []int64{1, 2}; !reflect.DeepEqual(store.cleared, want) {
		t.Fatalf("cleared servers = %v, want %v", store.cleared, want)
	}
}
