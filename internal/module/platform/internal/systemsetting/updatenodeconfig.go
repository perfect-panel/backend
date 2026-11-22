package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateNodeConfig stores the node settings, kept in the server settings
// category, and reloads the node subsystem. Masked outbound credentials keep
// the stored ones.
func (s *Service) UpdateNodeConfig(ctx context.Context, req *dto.NodeConfig) error {
	stored, err := s.storedNodeConfig(ctx)
	if err != nil {
		// A malformed stored document must not stop the update that fixes it.
		logger.WithContext(ctx).Errorw("[UpdateNodeConfig] stored node config could not be read", logger.Field("error", err.Error()))
	}
	var storedOutbounds []dto.PlatformNodeOutboundSnapshot
	if stored != nil {
		storedOutbounds = stored.Outbound
	}
	if err := keepOutboundSecrets(req.Outbound, storedOutbounds); err != nil {
		return err
	}
	change := settingsChange{category: "server", next: convertedConfigFields(*req)}
	if stored != nil {
		change.previous = convertedConfigFields(*stored)
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateNodeConfig] update node config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update server config error: %v", err)
	}
	return s.deps.reinit("node")
}
