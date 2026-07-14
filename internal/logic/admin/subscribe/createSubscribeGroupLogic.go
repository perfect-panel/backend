package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateSubscribeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create subscribe group
func NewCreateSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSubscribeGroupLogic {
	return &CreateSubscribeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateSubscribeGroupLogic) CreateSubscribeGroup(req *dto.CreateSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().CreateGroup(l.ctx, &subscribe.Group{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		l.Logger.Error("[CreateSubscribeGroupLogic] create subscribe group failed: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create subscribe group failed: %v", err.Error())
	}
	return nil
}
