package publicinfo

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// Heartbeat reports that the service is alive.
func (s *Service) Heartbeat(_ context.Context) (*dto.HeartbeatResponse, error) {
	return &dto.HeartbeatResponse{
		Status:    true,
		Message:   "service is alive",
		Timestamp: timeutil.Now().Unix(),
	}, nil
}
