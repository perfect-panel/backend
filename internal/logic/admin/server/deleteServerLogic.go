package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type DeleteServerLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewDeleteServerLogic Delete Server
func NewDeleteServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteServerLogic {
	return &DeleteServerLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteServerLogic) DeleteServer(req *dto.DeleteServerRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	if err := nodeStore.Transaction(l.ctx, func(db *gorm.DB) error {
		if err := nodeStore.DeleteServer(l.ctx, req.Id, db); err != nil {
			return err
		}
		return nodeStore.DeleteServerConfigOverride(l.ctx, req.Id, db)
	}); err != nil {
		l.Errorw("[DeleteServer] Delete Server Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "[DeleteServer] Delete Server Error")
	}
	return nodeStore.ClearNodeCache(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{req.Id},
		Search:   "",
	})
}
