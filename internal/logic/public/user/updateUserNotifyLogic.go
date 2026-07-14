package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateUserNotifyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update User Notify
func NewUpdateUserNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserNotifyLogic {
	return &UpdateUserNotifyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserNotifyLogic) UpdateUserNotify(req *dto.UpdateUserNotifyRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if u.Id == 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "user not login")
	}
	u.EnableLoginNotify = req.EnableLoginNotify
	u.EnableBalanceNotify = req.EnableBalanceNotify
	u.EnableSubscribeNotify = req.EnableSubscribeNotify
	u.EnableTradeNotify = req.EnableTradeNotify
	if err := l.svcCtx.Store.User().Update(l.ctx, u); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "update user notify error: %v", err.Error())
	}
	return nil
}
