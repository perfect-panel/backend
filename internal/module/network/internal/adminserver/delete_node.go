package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteNode removes a node and drops the node-facing caches of its server.
func (s *Service) DeleteNode(ctx context.Context, req *dto.DeleteNodeRequest) error {
	nodeStore := s.deps.Store.Node()
	data, err := nodeStore.FindOneNode(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[DeleteNode] Query Database Error: ", logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseQueryError, "[DeleteNode] Query Database Error")
	}

	if err := nodeStore.DeleteNode(ctx, req.Id); err != nil {
		logger.WithContext(ctx).Errorw("[DeleteNode] Delete Database Error: ", logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseDeletedError, "[DeleteNode] Delete Database Error")
	}

	return nodeStore.ClearServerCache(ctx, data.ServerId)
}
