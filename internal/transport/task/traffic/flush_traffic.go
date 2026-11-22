package traffic

import (
	"context"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/module/network"
	"github.com/perfect-panel/server/internal/transport/task/tasklock"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
)

const (
	trafficFlushLockKey = "traffic:flush:lock"
	trafficFlushLockTTL = 55 * time.Second
)

// FlushTrafficHandler flushes the network's due traffic buckets. The lock
// keeps two ticks, on this or another replica, from flushing the same buckets
// at once; its TTL stays under the one-minute schedule so a crashed run does
// not skip the next tick, and a run that outlives it keeps the lock through a
// heartbeat so no tick overlaps a slow flush.
type FlushTrafficHandler struct {
	deps Dependencies
}

// NewFlushTrafficHandler builds the flush over the network's aggregator.
func NewFlushTrafficHandler(deps Dependencies) *FlushTrafficHandler {
	return &FlushTrafficHandler{deps: deps}
}

func (h *FlushTrafficHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	lock, ok, err := tasklock.Acquire(ctx, h.deps.Redis, trafficFlushLockKey, trafficFlushLockTTL)
	if err != nil {
		return err
	}
	if !ok {
		logger.WithContext(ctx).Info("[FlushTraffic] another task is already running, skipping")
		return nil
	}
	defer releaseLock(ctx, lock, "[FlushTraffic]")
	defer lock.KeepAlive(ctx, trafficFlushLockTTL, reportLockHeartbeat(ctx, "[FlushTraffic]"))()
	return network.NewTrafficAggregator(h.deps.Aggregator).FlushDueBuckets(ctx, timeutil.Now())
}
