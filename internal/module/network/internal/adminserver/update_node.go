package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateNode stores a node's settings and drops the node-facing caches of
// its server, and of the server it moved from. A request without the enabled
// switch keeps the stored one: the column cannot be NULL.
func (s *Service) UpdateNode(ctx context.Context, req *dto.UpdateNodeRequest) error {
	nodeStore := s.deps.Store.Node()
	data, err := nodeStore.FindOneNode(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateNode] Query Database Error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find node %d", req.Id)
	}
	oldServerID := data.ServerId
	data.Name = req.Name
	data.Tags = slicesx.StringSliceToString(req.Tags)
	data.ServerId = req.ServerId
	data.Port = req.Port
	data.Address = req.Address
	data.Protocol = req.Protocol
	if req.Enabled != nil {
		data.Enabled = req.Enabled
	}
	if err := nodeStore.UpdateNode(ctx, data); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateNode] Update Database Error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update node %d", req.Id)
	}
	if err := nodeStore.ClearServerCache(ctx, oldServerID); err != nil {
		return err
	}
	if oldServerID != data.ServerId {
		return nodeStore.ClearServerCache(ctx, data.ServerId)
	}
	return nil
}
