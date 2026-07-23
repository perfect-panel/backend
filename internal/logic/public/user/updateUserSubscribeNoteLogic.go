package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateUserSubscribeNoteLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserSubscribeNoteLogic Update User Subscribe Note
func NewUpdateUserSubscribeNoteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserSubscribeNoteLogic {
	return &UpdateUserSubscribeNoteLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserSubscribeNoteLogic) UpdateUserSubscribeNote(req *dto.UpdateUserSubscribeNoteRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	userSub, err := l.svcCtx.Store.UserSubscription().FindOneUserSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Errorw("FindOneUserSubscribe failed:", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed: %v", err.Error())
	}

	if userSub.UserId != u.Id {
		l.Errorw("UserSubscribeId does not belong to the current user")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "UserSubscribeId does not belong to the current user")
	}

	userSub.Note = req.Note
	var newSub user.Subscribe
	tool.DeepCopy(&newSub, userSub)

	err = l.svcCtx.Store.UserSubscription().UpdateSubscribe(l.ctx, &newSub)
	if err != nil {
		l.Errorw("UpdateSubscribe failed:", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe failed: %v", err.Error())
	}

	// Clear user subscription cache
	if err = l.svcCtx.Store.UserCache().ClearSubscribeCache(l.ctx, &newSub); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("userSubscribeId", userSub.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	// Clear subscription cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	return nil
}
