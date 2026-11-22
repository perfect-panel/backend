package usersub

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteUserSubscribe deletes a locally managed subscription and drops its
// cached rows and its plan's node user lists.
func (s *Service) DeleteUserSubscribe(ctx context.Context, req *dto.DeleteUserSubscribeRequest) error {
	log := logger.WithContext(ctx)
	userSubscribe, err := s.deps.UserSubs.FindOneSubscribe(ctx, req.UserSubscribeId)
	if err != nil {
		log.Errorw("failed to find user subscribe", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "failed to find user subscribe: %v", err.Error())
	}

	if userSubscribe.EntitlementSource != "" {
		return usersub.ErrProviderManaged
	}
	err = s.deps.UserSubs.DeleteSubscribeById(ctx, req.UserSubscribeId)
	if err != nil {
		log.Errorw("failed to delete user subscribe", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "failed to delete user subscribe: %v", err.Error())
	}
	if err = s.deps.Cache.ClearSubscribeCache(ctx, userSubscribe); err != nil {
		log.Errorw("failed to clear user subscribe cache", logger.Field("error", err.Error()), logger.Field("userSubscribeId", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.ERROR, "failed to clear user subscribe cache: %v", err.Error())
	}
	if err = s.deps.Plans.ClearCache(ctx, userSubscribe.SubscribeId); err != nil {
		log.Errorw("failed to clear subscribe cache", logger.Field("error", err.Error()), logger.Field("subscribeId", userSubscribe.SubscribeId))
		return xerr.Wrapf(err, xerr.ERROR, "failed to clear subscribe cache: %v", err.Error())
	}
	return nil
}
