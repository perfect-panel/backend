package network

import "github.com/perfect-panel/backend/internal/module/network/internal/multiplier"

// MultiplierManager and MultiplierPeriod expose the network-owned runtime
// configuration to application assembly without exporting its implementation.
type MultiplierManager = multiplier.Manager
type MultiplierPeriod = multiplier.TimePeriod

// NewMultiplierManager returns a manager applying the multipliers of periods.
func NewMultiplierManager(periods []MultiplierPeriod) *MultiplierManager {
	return multiplier.NewManager(periods)
}
