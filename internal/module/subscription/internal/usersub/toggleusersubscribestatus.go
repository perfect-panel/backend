package usersub

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ToggleUserSubscribeStatus stops an active subscription or resumes a
// stopped one. The status is read under the row lock, so the toggle never
// undoes a status the lifecycle sweep or a refund wrote meanwhile.
func (s *Service) ToggleUserSubscribeStatus(ctx context.Context, req *dto.ToggleUserSubscribeStatusRequest) error {
	return s.changeStatus(ctx, req.UserSubscribeId, func(current uint8) (uint8, error) {
		target, ok := toggledStatus(current)
		if !ok {
			return 0, xerr.Errorf(xerr.SubscriptionStatusNotToggleable, "subscription %d has status %d", req.UserSubscribeId, current)
		}
		return target, nil
	})
}

// ChangeUserSubscribeStatus stops or resumes a subscription the caller saw
// in status from. It refuses when the status changed meanwhile, so an
// operator's confirmed "stop" can never become a resume because someone
// stopped it first.
func (s *Service) ChangeUserSubscribeStatus(ctx context.Context, id int64, from, to uint8) error {
	if target, ok := toggledStatus(from); !ok || target != to {
		return xerr.Errorf(xerr.SubscriptionStatusNotToggleable, "subscription %d cannot change from status %d to %d", id, from, to)
	}
	return s.changeStatus(ctx, id, func(current uint8) (uint8, error) {
		if current != from {
			return 0, xerr.Errorf(xerr.SubscriptionStatusChanged, "subscription %d is now in status %d, not %d", id, current, from)
		}
		return to, nil
	})
}

// toggledStatus is the other end of the stop/resume toggle, the only status
// change an operator makes by hand.
func toggledStatus(status uint8) (uint8, bool) {
	switch status {
	case usersub.SubscribeStatusActive:
		return usersub.SubscribeStatusStopped, true
	case usersub.SubscribeStatusStopped:
		return usersub.SubscribeStatusActive, true
	}
	return 0, false
}

// changeStatus writes the status next(current) returns, under the row lock,
// then drops the plan caches.
func (s *Service) changeStatus(ctx context.Context, id int64, next func(current uint8) (uint8, error)) error {
	var changed *usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		sub, err := store.UserSubscription().FindOneSubscribeForUpdate(ctx, id)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", id)
		}
		target, err := next(sub.Status)
		if err != nil {
			return err
		}
		sub.Status = target
		if err := store.UserSubscription().UpdateSubscribeColumns(ctx, sub, "status"); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update subscription %d", id)
		}
		changed = sub
		return nil
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[UserSubscribeStatus] change failed", logger.Field("error", xerr.Detail(err)), logger.Field("user_subscribe_id", id))
		return err
	}
	return s.clearPlanCaches(ctx, changed.SubscribeId)
}
