package network

import "github.com/perfect-panel/backend/internal/module/network/internal/trafficagg"

// TrafficAggregator is the network-owned pipeline shared by node reports and
// scheduled flush tasks. Queue adapters enter through this facade.
type TrafficAggregator = trafficagg.Aggregator
type TrafficAggregatorDeps = trafficagg.Deps
type UserTraffic = trafficagg.UserTraffic

// TrafficAggregatorStore is the pipeline's network persistence, and
// TrafficAggregatorAppStore the part of the application store that
// NewTrafficAggregatorStore adapts to it.
type (
	TrafficAggregatorStore    = trafficagg.Store
	TrafficAggregatorAppStore = trafficagg.AppStore
)

// NewTrafficAggregator returns the traffic pipeline over deps.
func NewTrafficAggregator(deps TrafficAggregatorDeps) *TrafficAggregator {
	return trafficagg.New(deps)
}

// NewTrafficAggregatorStore adapts the application store to the pipeline's
// network persistence.
func NewTrafficAggregatorStore(store TrafficAggregatorAppStore) TrafficAggregatorStore {
	return trafficagg.NewStore(store)
}
