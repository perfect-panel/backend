package selfsub

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	usermodel "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// PreUnsubscribe quotes the refund the owner would get for cancelling the
// subscription now.
func (s *Service) PreUnsubscribe(ctx context.Context, req *dto.PreUnsubscribeRequest) (*dto.PreUnsubscribeResponse, error) {
	u, ok := ctx.Value(requestctx.CtxKeyUser).(*usermodel.User)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}

	log := logger.WithContext(ctx)
	userSub, err := s.deps.UserSubs.FindOneSubscribe(ctx, req.Id)
	if err != nil {
		log.Errorw("[PreUnsubscribeLogic] FindOneSubscribe failed", logger.Field("err", err.Error()), logger.Field("reqId", req.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOneSubscribe failed: %v", err.Error())
	}
	if userSub.UserId != u.Id {
		log.Errorw("[PreUnsubscribeLogic] User subscribe does not belong to current user",
			logger.Field("userSubscribeId", userSub.Id),
			logger.Field("userId", u.Id))
		return nil, xerr.Errorf(xerr.InvalidAccess, "user subscribe does not belong to current user")
	}

	if userSub.EntitlementSource != "" {
		return nil, usersub.ErrProviderManaged
	}
	remainingAmount, err := s.remainingAmount(ctx, req.Id)
	if err != nil {
		log.Errorw("[PreUnsubscribeLogic] Calculate Remaining Amount Error:", logger.Field("err", err.Error()))
		return nil, err
	}
	return &dto.PreUnsubscribeResponse{
		DeductionAmount: remainingAmount,
	}, nil
}
