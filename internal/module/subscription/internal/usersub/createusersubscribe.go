package usersub

import (
	"context"
	"uuid"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateUserSubscribe gives a user an active subscription of the plan with
// the requested term; a traffic quota of 0 takes the plan's. In
// single-subscription mode a user holding a blocking subscription is
// refused.
func (s *Service) CreateUserSubscribe(ctx context.Context, req *dto.CreateUserSubscribeRequest) error {
	log := logger.WithContext(ctx)
	userInfo, err := s.deps.Users.FindOne(ctx, req.UserId)
	if err != nil {
		log.Errorw("FindOne error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOne error: %v", err.Error())
	}
	if s.deps.SingleModel() {
		hasBlockingSubscription, err := s.deps.UserSubs.HasBlockingSubscription(ctx, req.UserId)
		if err != nil {
			log.Errorw("HasBlockingSubscription error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "check user subscription error: %v", err.Error())
		}
		if hasBlockingSubscription {
			return xerr.Errorf(xerr.SingleSubscribeModeExceedsLimit, "Single subscribe mode exceeds limit")
		}
	}
	sub, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		log.Errorw("FindOne error", logger.Field("error", err.Error()), logger.Field("subscribeId", req.SubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOne error: %v", err.Error())
	}
	if req.Traffic == 0 {
		req.Traffic = sub.Traffic
	}

	userSub := usersub.Subscribe{
		UserId:      req.UserId,
		SubscribeId: req.SubscribeId,
		StartTime:   timeutil.Now(),
		ExpireTime:  usersub.ExpiryFromMilli(req.ExpiredAt),
		Traffic:     req.Traffic,
		Download:    0,
		Upload:      0,
		Token:       usersub.NewToken(),
		UUID:        uuid.NewV4().String(),
		Status:      usersub.SubscribeStatusActive,
	}
	if err = s.deps.UserSubs.InsertSubscribe(ctx, &userSub); err != nil {
		log.Errorw("InsertSubscribe error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "InsertSubscribe error: %v", err.Error())
	}

	err = s.deps.Users.ClearUserCacheOf(ctx, userInfo)
	if err != nil {
		log.Errorw("ClearUserCache error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "ClearUserCache error: %v", err.Error())
	}

	if err = s.deps.Plans.ClearCache(ctx, userSub.SubscribeId); err != nil {
		log.Errorw("ClearSubscribe error", logger.Field("error", err.Error()))
	}
	return nil
}
