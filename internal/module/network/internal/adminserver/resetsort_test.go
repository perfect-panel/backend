package adminserver

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// memorySorts holds the positions of both sortable lists, counts the sort
// transactions and records every update it is asked to store; updateErr
// fails them all.
type memorySorts struct {
	servers, nodes map[int64]int64
	updates        [][2]int64
	updateErr      error
	transactions   int
}

var (
	_ sortTransactor = (*memorySorts)(nil)
	_ sortRepo       = (*memorySorts)(nil)
)

func (m *memorySorts) InSortTx(_ context.Context, fn func(sortRepo) error) error {
	m.transactions++
	return fn(m)
}

func (m *memorySorts) QueryServerSorts(context.Context) ([]node.SortItem, error) {
	return sortItems(m.servers), nil
}

func (m *memorySorts) UpdateServerSort(_ context.Context, id, sort int64) error {
	return m.store(m.servers, id, sort)
}

func (m *memorySorts) QueryNodeSorts(context.Context) ([]node.SortItem, error) {
	return sortItems(m.nodes), nil
}

func (m *memorySorts) UpdateNodeSort(_ context.Context, id, sort int64) error {
	return m.store(m.nodes, id, sort)
}

func (m *memorySorts) store(positions map[int64]int64, id, sort int64) error {
	m.updates = append(m.updates, [2]int64{id, sort})
	if m.updateErr != nil {
		return m.updateErr
	}
	positions[id] = sort
	return nil
}

func sortItems(positions map[int64]int64) []node.SortItem {
	items := make([]node.SortItem, 0, len(positions))
	for id, sort := range positions {
		items = append(items, node.SortItem{Id: id, Sort: sort})
	}
	return items
}

// Only the positions that changed are stored, in the list being sorted and
// in one transaction; ids the list does not hold are ignored.
func TestResetSortStoresOnlyTheChangedPositions(t *testing.T) {
	req := &dto.ResetSortRequest{Sort: []dto.NetworkSortItem{{Id: 1, Sort: 1}, {Id: 2, Sort: 5}, {Id: 3, Sort: 4}, {Id: 9, Sort: 1}}}
	tests := []struct {
		name                   string
		list                   sortList
		wantUpdates            [][2]int64
		wantServers, wantNodes map[int64]int64
	}{
		{
			name:        "server",
			list:        serverSorts,
			wantUpdates: [][2]int64{{2, 5}, {3, 4}},
			wantServers: map[int64]int64{1: 1, 2: 5, 3: 4},
			wantNodes:   map[int64]int64{1: 1, 9: 9},
		},
		{
			name:        "node",
			list:        nodeSorts,
			wantUpdates: [][2]int64{{9, 1}},
			wantServers: map[int64]int64{1: 1, 2: 2, 3: 3},
			wantNodes:   map[int64]int64{1: 1, 9: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorts := &memorySorts{servers: map[int64]int64{1: 1, 2: 2, 3: 3}, nodes: map[int64]int64{1: 1, 9: 9}}
			if err := resetSort(context.Background(), sorts, tt.list, req); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(sorts.updates, tt.wantUpdates) {
				t.Fatalf("updates = %v, want %v", sorts.updates, tt.wantUpdates)
			}
			if !maps.Equal(sorts.servers, tt.wantServers) || !maps.Equal(sorts.nodes, tt.wantNodes) {
				t.Fatalf("servers = %v, nodes = %v, want %v and %v", sorts.servers, sorts.nodes, tt.wantServers, tt.wantNodes)
			}
			if sorts.transactions != 1 {
				t.Fatalf("transactions = %d, want 1", sorts.transactions)
			}
		})
	}
}

func TestResetSortReportsAFailedUpdateAsADatabaseError(t *testing.T) {
	cause := errors.New("write failed")
	sorts := &memorySorts{servers: map[int64]int64{1: 1}, updateErr: cause}
	req := &dto.ResetSortRequest{Sort: []dto.NetworkSortItem{{Id: 1, Sort: 2}}}

	err := resetSort(context.Background(), sorts, serverSorts, req)
	if !errors.Is(err, cause) || xerr.CodeOf(err) != xerr.DatabaseUpdateError {
		t.Fatalf("error = %v (code %d), want the write failure as a database update error", err, xerr.CodeOf(err))
	}
	if want := [][2]int64{{1, 2}}; !slices.Equal(sorts.updates, want) {
		t.Fatalf("updates = %v, want %v", sorts.updates, want)
	}
}
