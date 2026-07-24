package selfsub

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/order"

	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type ResetUserSubscribeTokenLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewResetUserSubscribeTokenLogic Reset User Subscribe Token
func newResetUserSubscribeTokenLogic(ctx context.Context, deps Deps) *ResetUserSubscribeTokenLogic {
	return &ResetUserSubscribeTokenLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *ResetUserSubscribeTokenLogic) ResetUserSubscribeToken(req *dto.ResetUserSubscribeTokenRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	userSub, err := l.deps.UserSubs.FindOneUserSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Errorw("FindOneUserSubscribe failed:", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed: %v", err.Error())
	}
	if userSub.UserId != u.Id {
		l.Errorw("UserSubscribeId does not belong to the current user")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "UserSubscribeId does not belong to the current user")
	}

	var orderDetails *order.Details
	// find order
	if userSub.OrderId != 0 {
		orderDetails, err = l.deps.Orders.FindOneDetails(l.ctx, userSub.OrderId)
		if err != nil {
			l.Errorw("FindOneDetails failed:", logger.Field("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneDetails failed: %v", err.Error())
		}
	} else {
		// if order id is 0, this a admin create user subscribe
		orderDetails = &order.Details{}
	}

	userSub.Token = uuidx.SubscribeToken(orderDetails.OrderNo + timeutil.Now().Format("20060102150405.000"))
	userSub.UUID = uuid.New().String()
	var newSub user.Subscribe
	tool.DeepCopy(&newSub, userSub)

	err = l.deps.UserSubs.UpdateSubscribe(l.ctx, &newSub)
	if err != nil {
		l.Errorw("UpdateSubscribe failed:", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe failed: %v", err.Error())
	}
	//clear user subscription cache
	if err = l.deps.Cache.ClearSubscribeCache(l.ctx, &newSub); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("userSubscribeId", userSub.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}
	// Clear subscription cache
	if err = l.deps.Plans.ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	return nil
}
