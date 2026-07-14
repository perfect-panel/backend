package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateNodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateNodeLogic Update Node
func NewUpdateNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateNodeLogic {
	return &UpdateNodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateNodeLogic) UpdateNode(req *dto.UpdateNodeRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	data, err := nodeStore.FindOneNode(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[UpdateNode] Query Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "[UpdateNode] Query Database Error")
	}
	data.Name = req.Name
	data.Tags = tool.StringSliceToString(req.Tags)
	data.ServerId = req.ServerId
	data.Port = req.Port
	data.Address = req.Address
	data.Protocol = req.Protocol
	data.Enabled = req.Enabled
	err = nodeStore.UpdateNode(l.ctx, data)
	if err != nil {
		l.Errorw("[UpdateNode] Update Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "[UpdateNode] Update Database Error")
	}
	return nodeStore.ClearNodeCache(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{data.ServerId},
		Search:   "",
	})
}
