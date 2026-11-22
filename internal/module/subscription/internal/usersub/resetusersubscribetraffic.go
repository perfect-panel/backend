package usersub

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ResetUserSubscribeTraffic clears the subscription's traffic counters like
// every other traffic reset: an exhausted subscription inside its term is
// active again, a hold or an expired term stays.
func (s *Service) ResetUserSubscribeTraffic(ctx context.Context, req *dto.ResetUserSubscribeTrafficRequest) error {
	var reset *usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		sub, err := store.UserSubscription().FindOneSubscribeForUpdate(ctx, req.UserSubscribeId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", req.UserSubscribeId)
		}
		if err := store.UserSubscription().UpdateSubscribeColumns(ctx, sub, sub.ResetTraffic(timeutil.Now())...); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "reset traffic of subscription %d", req.UserSubscribeId)
		}
		reset = sub
		return nil
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[ResetUserSubscribeTraffic] Reset failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return err
	}
	return s.clearPlanCaches(ctx, reset.SubscribeId)
}
