package tool

import (
	"context"

	"github.com/perfect-panel/backend/pkg/logger"
)

// RestartSystem restarts the transport server in the background and returns
// at once: the restart replaces the server answering this request. Its
// outcome is only logged.
func (s *Service) RestartSystem(ctx context.Context) error {
	log := logger.WithContext(ctx)
	log.Info("[RestartSystem]", logger.Field("info", "Restarting system"))
	go func() {
		if err := s.deps.Restart(); err != nil {
			log.Errorw("[RestartSystem]", logger.Field("error", err.Error()))
		}
		log.Info("[RestartSystem]", logger.Field("info", "System restarted"))
	}()
	return nil
}
