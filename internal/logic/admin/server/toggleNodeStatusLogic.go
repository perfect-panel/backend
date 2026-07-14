package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type ToggleNodeStatusLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewToggleNodeStatusLogic Toggle Node Status
func NewToggleNodeStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ToggleNodeStatusLogic {
	return &ToggleNodeStatusLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ToggleNodeStatusLogic) ToggleNodeStatus(req *dto.ToggleNodeStatusRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	data, err := nodeStore.FindOneNode(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[ToggleNodeStatus] Query Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[ToggleNodeStatus] Query Database Error")
	}
	data.Enabled = req.Enable

	err = nodeStore.UpdateNode(l.ctx, data)
	if err != nil {
		l.Errorw("[ToggleNodeStatus] Update Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "[ToggleNodeStatus] Update Database Error")
	}

	return nodeStore.ClearNodeCache(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{data.ServerId},
		Tag:      strings.Split(data.Tags, ","),
		Search:   "",
	})
}
