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

type CreateNodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreateNodeLogic Create Node
func NewCreateNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateNodeLogic {
	return &CreateNodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateNodeLogic) CreateNode(req *dto.CreateNodeRequest) error {
	data := node.Node{
		Name:     req.Name,
		Tags:     tool.StringSliceToString(req.Tags),
		Enabled:  req.Enabled,
		Port:     req.Port,
		Address:  req.Address,
		ServerId: req.ServerId,
		Protocol: req.Protocol,
	}
	err := l.svcCtx.Store.Node().InsertNode(l.ctx, &data)
	if err != nil {
		l.Errorw("[CreateNode] Insert Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "[CreateNode] Insert Database Error")
	}

	return nil
}
