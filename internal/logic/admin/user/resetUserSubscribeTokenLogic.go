package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type ResetUserSubscribeTokenLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetUserSubscribeTokenLogic Reset user subscribe token
func NewResetUserSubscribeTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserSubscribeTokenLogic {
	return &ResetUserSubscribeTokenLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetUserSubscribeTokenLogic) ResetUserSubscribeToken(req *dto.ResetUserSubscribeTokenRequest) error {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		logger.Errorf("[ResetUserSubscribeToken] FindOneSubscribe error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribe error: %v", err.Error())
	}
	userSub.Token = uuidx.SubscribeToken(fmt.Sprintf("AdminUpdate:%d", timeutil.Now().UnixMilli()))

	err = l.svcCtx.Store.User().UpdateSubscribe(l.ctx, userSub)
	if err != nil {
		logger.Errorf("[ResetUserSubscribeToken] UpdateSubscribe error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe error: %v", err.Error())
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
