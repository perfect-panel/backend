package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateNode stores a new node of a server.
func (s *Service) CreateNode(ctx context.Context, req *dto.CreateNodeRequest) error {
	data := node.Node{
		Name:     req.Name,
		Tags:     slicesx.StringSliceToString(req.Tags),
		Enabled:  req.Enabled,
		Port:     req.Port,
		Address:  req.Address,
		ServerId: req.ServerId,
		Protocol: req.Protocol,
	}
	if err := s.deps.Store.Node().InsertNode(ctx, &data); err != nil {
		logger.WithContext(ctx).Errorw("[CreateNode] Insert Database Error: ", logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseInsertError, "[CreateNode] Insert Database Error")
	}
	return nil
}
