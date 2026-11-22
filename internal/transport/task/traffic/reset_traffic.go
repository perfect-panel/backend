package traffic

import (
	"context"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/transport/task/tasklock"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	resetTrafficLockKey = "reset_traffic_lock"
	resetTrafficLockTTL = 5 * time.Minute
)

// CalendarTrafficResetter is the subscription module's calendar reset.
type CalendarTrafficResetter interface {
	ResetCalendarTraffic(ctx context.Context) error
}

// ResetTrafficHandler is the queue shell of the calendar traffic reset: the
// reset rules and their once-per-day guarantee live in the subscription
// module. A failed run returns its error and asynq retries it (the scheduler
// sets the retry budget); the lock only keeps two runs from overlapping, and
// a heartbeat keeps it through a run longer than its TTL.
type ResetTrafficHandler struct {
	resetter CalendarTrafficResetter
	redis    *redis.Client
}

// NewResetTrafficHandler builds the shell over the subscription facade and
// the Redis connection holding the run lock.
func NewResetTrafficHandler(resetter CalendarTrafficResetter, rdb *redis.Client) *ResetTrafficHandler {
	return &ResetTrafficHandler{resetter: resetter, redis: rdb}
}

func (h *ResetTrafficHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	lock, ok, err := tasklock.Acquire(ctx, h.redis, resetTrafficLockKey, resetTrafficLockTTL)
	if err != nil {
		return err
	}
	if !ok {
		logger.WithContext(ctx).Info("[ResetTraffic] Another run holds the lock, skipping")
		return nil
	}
	defer releaseLock(ctx, lock, "[ResetTraffic]")
	defer lock.KeepAlive(ctx, resetTrafficLockTTL, reportLockHeartbeat(ctx, "[ResetTraffic]"))()
	return h.resetter.ResetCalendarTraffic(ctx)
}

// releaseLock frees a task lock; a failure only delays the next run until
// the lock expires.
func releaseLock(ctx context.Context, lock *tasklock.Lock, tag string) {
	if _, err := lock.Release(ctx); err != nil {
		logger.WithContext(ctx).Errorw(tag+" Release lock failed", logger.Field("error", err.Error()))
	}
}

// reportLockHeartbeat logs a failed lock heartbeat. A lost lock means the
// run outlived its TTL and a replica may be running the same task; the log
// line is what makes that visible.
func reportLockHeartbeat(ctx context.Context, tag string) func(error) {
	return func(err error) {
		logger.WithContext(ctx).Errorw(tag+" Lock heartbeat failed", logger.Field("error", err.Error()))
	}
}
