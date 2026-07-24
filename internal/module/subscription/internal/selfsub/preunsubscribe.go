package selfsub

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	usermodel "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type PreUnsubscribeLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewPreUnsubscribeLogic Pre Unsubscribe
func newPreUnsubscribeLogic(ctx context.Context, deps Deps) *PreUnsubscribeLogic {
	return &PreUnsubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *PreUnsubscribeLogic) PreUnsubscribe(req *dto.PreUnsubscribeRequest) (resp *dto.PreUnsubscribeResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*usermodel.User)
	if !ok {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	userSub, err := l.deps.UserSubs.FindOneSubscribe(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[PreUnsubscribeLogic] FindOneSubscribe failed", logger.Field("err", err.Error()), logger.Field("reqId", req.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribe failed: %v", err.Error())
	}
	if userSub.UserId != u.Id {
		l.Errorw("[PreUnsubscribeLogic] User subscribe does not belong to current user",
			logger.Field("userSubscribeId", userSub.Id),
			logger.Field("userId", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "user subscribe does not belong to current user")
	}

	remainingAmount, err := CalculateRemainingAmount(l.ctx, l.deps, req.Id)
	if err != nil {
		l.Errorw("[PreUnsubscribeLogic] Calculate Remaining Amount Error:", logger.Field("err", err.Error()))
		return nil, err
	}
	return &dto.PreUnsubscribeResponse{
		DeductionAmount: remainingAmount,
	}, nil
}
