// Package subscription holds the queue handlers of the scheduled
// subscription tasks: the lifecycle sweep and the pre-expiry reminder. The
// business rules live in the subscription module; the handlers only run it.
package subscription

import (
	"context"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/transport/task/tasklock"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	// checkSubscriptionLockKey is the Redis lock one sweep holds across the
	// replicas; a run that dies with it frees it after
	// checkSubscriptionLockTTL, well after a sweep should have finished.
	checkSubscriptionLockKey = "check_subscription_lock"
	checkSubscriptionLockTTL = 5 * time.Minute
)

// LifecycleSweeper is the subscription module's lifecycle sweep.
type LifecycleSweeper interface {
	CheckSubscriptions(ctx context.Context) error
}

// CheckSubscriptionHandler is the queue shell of the subscription lifecycle
// sweep. The sweep runs every minute and notifies the owners of the
// subscriptions it finishes, so a run that outlasts the tick must not overlap
// the next one: both would select the same rows and the owners would hear
// twice. The lock keeps the runs apart; a failed run returns its error and
// asynq retries it.
type CheckSubscriptionHandler struct {
	sweeper LifecycleSweeper
	redis   *redis.Client
}

// NewCheckSubscriptionHandler builds the shell over the subscription facade
// and the Redis connection holding the run lock.
func NewCheckSubscriptionHandler(sweeper LifecycleSweeper, rdb *redis.Client) *CheckSubscriptionHandler {
	return &CheckSubscriptionHandler{sweeper: sweeper, redis: rdb}
}

func (h *CheckSubscriptionHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	lock, ok, err := tasklock.Acquire(ctx, h.redis, checkSubscriptionLockKey, checkSubscriptionLockTTL)
	if err != nil {
		return err
	}
	if !ok {
		logger.WithContext(ctx).Info("[CheckSubscription] Another run holds the lock, skipping")
		return nil
	}
	defer func() {
		// A failed release only delays the next run until the lock expires.
		if _, err := lock.Release(ctx); err != nil {
			logger.WithContext(ctx).Errorw("[CheckSubscription] Release lock failed", logger.Field("error", err.Error()))
		}
	}()
	return h.sweeper.CheckSubscriptions(ctx)
}
