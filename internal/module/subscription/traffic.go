package subscription

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/trafficusage"
)

// TrafficUsageStore restricts usage accounting to subscription transactions
// and the durable inbox that deduplicates the buckets.
type TrafficUsageStore = trafficusage.Store

// TrafficUsage charges the network's traffic buckets to the subscriptions'
// usage, each bucket at most once.
type TrafficUsage interface {
	ApplyBucketOnce(ctx context.Context, bucket string, deltas []traffic.SubscribeTrafficDelta) error
}

// NewTrafficUsage returns the usage accounting over its store.
func NewTrafficUsage(store TrafficUsageStore) TrafficUsage {
	return trafficusage.New(store)
}
