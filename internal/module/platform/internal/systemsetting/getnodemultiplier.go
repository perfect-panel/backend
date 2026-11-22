package systemsetting

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetNodeMultiplier returns the stored node traffic multiplier periods. A
// malformed document is reported as an error.
func (s *Service) GetNodeMultiplier(ctx context.Context) (*dto.GetNodeMultiplierResponse, error) {
	data, err := s.deps.System.FindNodeMultiplierConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetNodeMultiplier] query the node multiplier config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Get Node Multiplier Config Error: %s", err.Error())
	}
	var periods []dto.TimePeriod
	if data.Value != "" {
		if err := json.Unmarshal([]byte(data.Value), &periods); err != nil {
			logger.WithContext(ctx).Errorw("[GetNodeMultiplier] decode the node multiplier config failed", logger.Field("error", err.Error()), logger.Field("value", data.Value))
			return nil, xerr.Wrapf(err, xerr.ERROR, "Unmarshal Node Multiplier Config Error: %s", err.Error())
		}
	}
	return &dto.GetNodeMultiplierResponse{Periods: periods}, nil
}
