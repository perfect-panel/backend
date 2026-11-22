package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateSubscribeConfig stores the subscription settings. A new subscribe
// path needs the HTTP routes rebuilt, so the server restarts in the
// background instead of reloading the subscribe subsystem; a failed restart
// is only logged, the settings being stored.
func (s *Service) UpdateSubscribeConfig(ctx context.Context, req *dto.SubscribeConfig) error {
	log := logger.WithContext(ctx)
	change := settingsChange{
		category: "subscribe",
		next:     convertedConfigFields(*req),
		previous: previousFields(ctx, "subscribe", s.GetSubscribeConfig, convertedConfigFields),
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		log.Errorw("[UpdateSubscribeConfig] update subscribe config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update subscribe config error: %v", err)
	}

	if s.deps.subscribePath() != req.SubscribePath {
		go func() {
			if err := s.deps.restart(); err != nil {
				log.Errorw("[UpdateSubscribeConfig] restart error", logger.Field("error", err.Error()))
			}
		}()
		return nil
	}
	return s.deps.reinit("subscribe")
}
