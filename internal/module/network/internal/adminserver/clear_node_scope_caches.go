package adminserver

import (
	"context"
	"errors"
	"slices"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ClearServerCachesByNodeScope drops the node-facing caches (user lists and
// configs) of every server carrying a node the scope selects: an explicit
// node id or any of the tags, whether the node is enabled or not. One query
// resolves the servers however many plans the scope came from; an empty
// scope clears nothing.
func (s *Service) ClearServerCachesByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) error {
	return clearServerCachesByNodeScope(ctx, s.deps.Store.Node(), nodeIDs, tags)
}

// scopeCacheStore resolves the nodes of a scope and drops their servers'
// caches.
type scopeCacheStore interface {
	ListNodesByScope(ctx context.Context, nodeIDs []int64, tags []string, enabled *bool, preload bool) ([]*node.Node, error)
	ClearServerCache(ctx context.Context, serverId int64) error
}

func clearServerCachesByNodeScope(ctx context.Context, nodes scopeCacheStore, nodeIDs []int64, tags []string) error {
	tags = slices.DeleteFunc(slices.Clone(tags), func(tag string) bool { return tag == "" })
	if len(nodeIDs) == 0 && len(tags) == 0 {
		// The scope query without selectors would list every node.
		return nil
	}
	list, err := nodes.ListNodesByScope(ctx, nodeIDs, tags, nil, false)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "list the nodes of the scope")
	}
	seen := make(map[int64]struct{}, len(list))
	serverIDs := make([]int64, 0, len(list))
	for _, item := range list {
		if item == nil || item.ServerId == 0 {
			continue
		}
		if _, ok := seen[item.ServerId]; !ok {
			seen[item.ServerId] = struct{}{}
			serverIDs = append(serverIDs, item.ServerId)
		}
	}
	slices.Sort(serverIDs)
	var errs []error
	for _, serverID := range serverIDs {
		if err := nodes.ClearServerCache(ctx, serverID); err != nil {
			errs = append(errs, xerr.Wrapf(err, xerr.ERROR, "clear the caches of server %d", serverID))
		}
	}
	return errors.Join(errs...)
}
