package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// PreViewNodeMultiplier reports the node traffic multiplier in effect now.
func (s *Service) PreViewNodeMultiplier(_ context.Context) (*dto.PreViewNodeMultiplierResponse, error) {
	now := timeutil.Now()
	return &dto.PreViewNodeMultiplierResponse{
		Ratio:       s.deps.multiplier(now),
		CurrentTime: now.Format("2006-01-02 15:04:05"),
	}, nil
}
