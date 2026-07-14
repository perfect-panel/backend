package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type ResetUserSubscribeTrafficLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetUserSubscribeTrafficLogic Reset user subscribe traffic
func NewResetUserSubscribeTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserSubscribeTrafficLogic {
	return &ResetUserSubscribeTrafficLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetUserSubscribeTrafficLogic) ResetUserSubscribeTraffic(req *dto.ResetUserSubscribeTrafficRequest) error {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Errorw("FindOneSubscribe error", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " FindOneSubscribe error: %v", err.Error())
	}
	userSub.Download = 0
	userSub.Upload = 0

	err = l.svcCtx.Store.User().UpdateSubscribe(l.ctx, userSub)
	if err != nil {
		l.Errorw("UpdateSubscribe error", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), " UpdateSubscribe error: %v", err.Error())
	}
	// Clear user subscribe cache
	if err = l.svcCtx.Store.User().ClearSubscribeCache(l.ctx, userSub); err != nil {
		l.Errorw("ClearSubscribeCache failed:", logger.Field("error", err.Error()), logger.Field("userSubscribeId", userSub.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}
	// Clear subscribe cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Errorw("failed to clear subscribe cache", logger.Field("error", err.Error()), logger.Field("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear subscribe cache: %v", err.Error())
	}
	return nil
}
