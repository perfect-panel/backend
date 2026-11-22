package sweep

import (
	"context"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

const (
	// expiryReminderWindow is how far ahead a subscription is announced as
	// expiring, giving the owner time to renew before service stops.
	expiryReminderWindow = 3 * 24 * time.Hour
	// expiryReminderConsumer keys the reminder's idempotency marker. The
	// event key carries the expiry timestamp, so a renewed subscription
	// becomes eligible again while an unchanged one is announced only once —
	// a subscription sits in the window for days and must not be nagged
	// daily.
	expiryReminderConsumer = "notification.subscription_expiring"
)

// RemindExpiringSubscribes announces subscriptions about to expire. A
// subscription that cannot be announced is skipped rather than failing the
// pass: nothing here changes subscription state, and the next daily run
// retries it while the expiry stays inside the window.
func (s *Service) RemindExpiringSubscribes(ctx context.Context) error {
	if s.deps.Notify == nil {
		return nil
	}
	now := timeutil.Now()
	subs, err := s.deps.UserSubs.FindExpiringSubscribes(ctx, now, now.Add(expiryReminderWindow))
	if err != nil {
		logger.Errorw("[RemindExpiring] Query subscribe failed", logger.Field("error", err.Error()))
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	reminded := 0
	for _, sub := range subs {
		eventKey := strconv.FormatInt(sub.Id, 10) + ":" + strconv.FormatInt(sub.ExpireTime.UnixMilli(), 10)
		mark, err := s.deps.Store.Inbox().Find(ctx, expiryReminderConsumer, eventKey)
		if err != nil {
			logger.Errorw("[RemindExpiring] Read reminder marker failed",
				logger.Field("error", err.Error()),
				logger.Field("user_subscribe_id", sub.Id),
			)
			continue
		}
		if mark != nil {
			continue
		}
		if !s.ownerExists(ctx, sub.UserId) {
			continue
		}

		planName, renewalAmount := s.planSummary(ctx, sub.SubscribeId)
		s.deps.Notify.NotifySubscriptionExpiring(ctx, sub.UserId, planName, sub.ExpireTime, renewalAmount)

		// The marker is written after the notice so a failure to record it
		// costs a duplicate reminder rather than a silent miss.
		if err := s.deps.Store.Inbox().Insert(ctx, expiryReminderConsumer, eventKey, ""); err != nil {
			logger.Errorw("[RemindExpiring] Record reminder marker failed",
				logger.Field("error", err.Error()),
				logger.Field("user_subscribe_id", sub.Id),
			)
		}
		reminded++
	}
	logger.Infow("[RemindExpiring] Reminded owners",
		logger.Field("candidates", int64(len(subs))),
		logger.Field("reminded", int64(reminded)),
	)
	return nil
}

// ownerExists reports whether the subscription's owner still has an account.
// Deleting a user only soft-deletes the user row, so its subscriptions keep
// reaching this sweep. An owner whose state cannot be read is skipped like
// any other subscription that cannot be announced.
func (s *Service) ownerExists(ctx context.Context, userID int64) bool {
	if s.deps.Owners == nil {
		return false
	}
	state, err := s.deps.Owners.FindAccountState(ctx, userID)
	if err != nil {
		logger.Errorw("[RemindExpiring] Read owner state failed",
			logger.Field("error", err.Error()),
			logger.Field("user_id", userID),
		)
		return false
	}
	return !state.DeletedAt.Valid
}

// planSummary resolves the plan's display name and renewal price. A plan that
// cannot be read still yields a usable notice.
func (s *Service) planSummary(ctx context.Context, planID int64) (name string, renewalAmount int64) {
	plan, err := s.deps.Plans.FindOne(ctx, planID)
	if err != nil || plan == nil {
		logger.Infow("[RemindExpiring] Plan unavailable",
			logger.Field("subscribe_id", planID),
		)
		return "", 0
	}
	return plan.Name, plan.UnitPrice
}
