package system

import (
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
)

// ParseNodeConfig decodes the node configuration stored in the "server"
// settings rows. Startup, the admin reload and the admin settings read all
// parse it here, so they agree on every rule.
//
// DNS and Outbound hold JSON documents; a malformed one is reported rather
// than dropped. A scalar setting that does not decode keeps its zero value
// and Block is decoded leniently, as they always have been; both are logged
// with the setting's key.
func ParseNodeConfig(rows []*System) (config.NodeConfig, error) {
	var stored config.NodeDBConfig
	if err := config.DecodeSystemConfig(rows, &stored); err != nil {
		logger.Errorw("[NodeConfig] stored node settings could not be applied, the affected fields keep their zero value",
			logger.Field("error", err.Error()))
	}
	parsed := config.NodeConfig{
		NodeSecret:             stored.NodeSecret,
		NodePullInterval:       stored.NodePullInterval,
		NodePushInterval:       stored.NodePushInterval,
		TrafficReportThreshold: stored.TrafficReportThreshold,
		IPStrategy:             stored.IPStrategy,
	}
	if stored.DNS != "" {
		if err := json.Unmarshal([]byte(stored.DNS), &parsed.DNS); err != nil {
			return config.NodeConfig{}, fmt.Errorf("node setting DNS: %w", err)
		}
	}
	if stored.Block != "" {
		var block []string
		if err := json.Unmarshal([]byte(stored.Block), &block); err != nil {
			// An unreadable list blocks nothing, as before.
			logger.Errorw("[NodeConfig] the Block node setting is not a JSON string list, blocking nothing",
				logger.Field("key", "Block"), logger.Field("error", err.Error()))
		}
		parsed.Block = slicesx.RemoveDuplicateElements(block...)
	}
	if stored.Outbound != "" {
		if err := json.Unmarshal([]byte(stored.Outbound), &parsed.Outbound); err != nil {
			return config.NodeConfig{}, fmt.Errorf("node setting Outbound: %w", err)
		}
	}
	return parsed, nil
}
