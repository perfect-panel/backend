package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ToggleNodeStatus enables or disables a node and drops the node-facing
// caches of its server. A request without the switch keeps the stored one,
// by UpdateNode's rule: the column cannot be NULL.
func (s *Service) ToggleNodeStatus(ctx context.Context, req *dto.ToggleNodeStatusRequest) error {
	if req.Id <= 0 {
		return xerr.Errorf(xerr.InvalidParams, "toggle node status: node id is required")
	}
	nodeStore := s.deps.Store.Node()
	data, err := nodeStore.FindOneNode(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[ToggleNodeStatus] Query Database Error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find node %d", req.Id)
	}
	if req.Enable != nil {
		data.Enabled = req.Enable
	}
	if err := nodeStore.UpdateNode(ctx, data); err != nil {
		logger.WithContext(ctx).Errorw("[ToggleNodeStatus] Update Database Error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update node %d", req.Id)
	}

	return nodeStore.ClearServerCache(ctx, data.ServerId)
}
