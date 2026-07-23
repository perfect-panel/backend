package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateUserSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserSubscribeLogic Update user subscribe
func NewUpdateUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserSubscribeLogic {
	return &UpdateUserSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserSubscribeLogic) UpdateUserSubscribe(req *dto.UpdateUserSubscribeRequest) error {
	userSub, err := l.svcCtx.Store.UserSubscription().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Errorw("FindOneUserSubscribe failed:", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed: %v", err.Error())
	}
	expiredAt := time.UnixMilli(req.ExpiredAt)
	if time.Since(expiredAt).Minutes() > 0 {
		userSub.Status = 3
	} else {
		userSub.Status = 1
	}

	err = l.svcCtx.Store.UserSubscription().UpdateSubscribe(l.ctx, &user.Subscribe{
		Id:          userSub.Id,
		UserId:      userSub.UserId,
		OrderId:     userSub.OrderId,
		SubscribeId: req.SubscribeId,
		StartTime:   userSub.StartTime,
		ExpireTime:  time.UnixMilli(req.ExpiredAt),
		Traffic:     req.Traffic,
		Download:    req.Download,
		Upload:      req.Upload,
		Token:       userSub.Token,
		UUID:        userSub.UUID,
		Status:      userSub.Status,
	})

	if err != nil {
		l.Errorw("UpdateSubscribe failed:", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe failed: %v", err.Error())
	}
	// Clear user subscribe cache
	if err = l.svcCtx.Store.UserCache().ClearSubscribeCache(l.ctx, userSub); err != nil {
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
