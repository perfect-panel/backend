package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateUserSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create user subcribe
func NewCreateUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserSubscribeLogic {
	return &CreateUserSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserSubscribeLogic) CreateUserSubscribe(req *dto.CreateUserSubscribeRequest) error {
	// validate user
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, req.UserId)
	if err != nil {
		l.Errorw("FindOne error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %v", err.Error())
	}
	if l.svcCtx.Config.Subscribe.SingleModel {
		hasBlockingSubscription, err := l.svcCtx.Store.User().HasBlockingSubscription(l.ctx, req.UserId)
		if err != nil {
			l.Errorw("HasBlockingSubscription error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check user subscription error: %v", err.Error())
		}
		if hasBlockingSubscription {
			return errors.Wrapf(xerr.NewErrCode(xerr.SingleSubscribeModeExceedsLimit), "Single subscribe mode exceeds limit")
		}
	}
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.SubscribeId)
	if err != nil {
		l.Errorw("FindOne error", logger.Field("error", err.Error()), logger.Field("subscribeId", req.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %v", err.Error())
	}
	if req.Traffic == 0 {
		req.Traffic = sub.Traffic
	}

	userSub := user.Subscribe{
		UserId:      req.UserId,
		SubscribeId: req.SubscribeId,
		StartTime:   timeutil.Now(),
		ExpireTime:  time.UnixMilli(req.ExpiredAt),
		Traffic:     req.Traffic,
		Download:    0,
		Upload:      0,
		Token:       uuidx.SubscribeToken(fmt.Sprintf("adminCreate:%d", timeutil.Now().UnixMilli())),
		UUID:        uuid.New().String(),
		Status:      user.SubscribeStatusActive,
	}
	if err = l.svcCtx.Store.User().InsertSubscribe(l.ctx, &userSub); err != nil {
		l.Errorw("InsertSubscribe error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertSubscribe error: %v", err.Error())
	}

	err = l.svcCtx.Store.User().UpdateUserCache(l.ctx, userInfo)
	if err != nil {
		l.Errorw("UpdateUserCache error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "UpdateUserCache error: %v", err.Error())
	}

	err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId)
	if err != nil {
		logger.Errorw("ClearSubscribe error", logger.Field("error", err.Error()))
	}
	return nil
}
