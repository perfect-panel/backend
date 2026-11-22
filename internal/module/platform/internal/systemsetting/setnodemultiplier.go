package systemsetting

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// nodeMultiplierKey is the server setting holding the multiplier periods.
const nodeMultiplierKey = "NodeMultiplierConfig"

// SetNodeMultiplier stores the node traffic multiplier periods, with the
// audit row of the change, and reloads the node subsystem, which evaluates
// them.
func (s *Service) SetNodeMultiplier(ctx context.Context, req *dto.SetNodeMultiplierRequest) error {
	data, err := json.Marshal(req.Periods)
	if err != nil {
		logger.WithContext(ctx).Errorw("[SetNodeMultiplier] encode the node multiplier config failed", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.ERROR, "Marshal Node Multiplier Config Error: %s", err.Error())
	}
	change := settingsChange{category: "server", next: []configFieldValue{{key: nodeMultiplierKey, value: string(data), valueType: "string"}}}
	if stored, err := s.deps.System.FindNodeMultiplierConfig(ctx); err == nil && stored != nil && stored.Key == nodeMultiplierKey {
		change.previous = []configFieldValue{{key: nodeMultiplierKey, value: stored.Value, valueType: "string"}}
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[SetNodeMultiplier] update the node multiplier config failed", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "Update Node Multiplier Config Error: %s", err.Error())
	}
	return s.deps.reinit("node")
}
