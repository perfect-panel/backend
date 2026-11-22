package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteServer removes a server together with its nodes and its node
// configuration override, in one network transaction, and then drops its
// node-facing caches. The nodes carry no foreign key to their server: left
// behind, they would fail every subscription render that selects them.
func (s *Service) DeleteServer(ctx context.Context, req *dto.DeleteServerRequest) error {
	if err := s.deps.Store.InNetworkTx(ctx, func(store repository.NetworkStore) error {
		nodeStore := store.Node()
		nodes, err := nodeStore.ListNodes(ctx, &node.FilterNodeParams{ServerId: []int64{req.Id}})
		if err != nil {
			return err
		}
		for _, item := range nodes {
			if err := nodeStore.DeleteNode(ctx, item.Id); err != nil {
				return err
			}
		}
		if err := nodeStore.DeleteServer(ctx, req.Id); err != nil {
			return err
		}
		return nodeStore.DeleteServerConfigOverride(ctx, req.Id)
	}); err != nil {
		logger.WithContext(ctx).Errorw("[DeleteServer] Delete Server Error: ", logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseDeletedError, "[DeleteServer] Delete Server Error")
	}
	return s.deps.Store.Node().ClearServerCache(ctx, req.Id)
}
