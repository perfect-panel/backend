package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteUserSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewDeleteUserSubscribeLogic Delete user subcribe
func NewDeleteUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserSubscribeLogic {
	return &DeleteUserSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserSubscribeLogic) DeleteUserSubscribe(req *dto.DeleteUserSubscribeRequest) error {
	// find user subscribe by ID
	userSubscribe, err := l.svcCtx.Store.UserSubscription().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Errorw("failed to find user subscribe", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to find user subscribe: %v", err.Error())
	}

	err = l.svcCtx.Store.UserSubscription().DeleteSubscribeById(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Errorw("failed to delete user subscribe", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "failed to delete user subscribe: %v", err.Error())
	}
	// Clear user subscribe cache
	if err = l.svcCtx.Store.UserCache().ClearSubscribeCache(l.ctx, userSubscribe); err != nil {
		l.Errorw("failed to clear user subscribe cache", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear user subscribe cache: %v", err.Error())
	}
	// Clear subscribe cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSubscribe.SubscribeId); err != nil {
		l.Errorw("failed to clear subscribe cache", logger.Field("error", err.Error()), logger.Field("subscribeId", userSubscribe.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear subscribe cache: %v", err.Error())
	}
	return nil
}
