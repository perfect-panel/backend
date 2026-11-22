// Package traffic holds the queue handlers of the traffic tasks: flushing the
// aggregated node traffic, the calendar traffic reset, the daily traffic
// statistics and the log retention. Each handler runs its module and returns
// the module's error, so asynq owns the retries.
package traffic

import (
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/redis/go-redis/v9"
)

// Dependencies are the traffic tasks' dependencies. The flush runs the
// network's traffic aggregator; the daily statistics and the log retention
// are module calls.
type Dependencies struct {
	Redis      *redis.Client
	Aggregator network.TrafficAggregatorDeps
	// Statistics records the daily traffic statistics (the network facade);
	// Logs applies the log retention (the platform facade).
	Statistics StatRecorder
	Logs       LogCleaner
}
