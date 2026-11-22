package adminserver

import (
	"context"
	"fmt"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/internal/nodeconfig"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateServerNodeConfig stores a server's node configuration override, or
// removes it when every value is inherited, and drops the server's
// node-facing caches.
func (s *Service) UpdateServerNodeConfig(ctx context.Context, req *dto.UpdateServerNodeConfigRequest) error {
	log := logger.WithContext(ctx)
	nodeStore := s.deps.Store.Node()
	if _, err := nodeStore.FindOneServer(ctx, req.ServerID); err != nil {
		log.Errorf("[UpdateServerNodeConfig] FindOneServer Error: %v", err.Error())
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find server error: %v", err)
	}

	data, allInherited, err := nodeconfig.OverrideModel(req.ServerID, req.ServerNodeConfigOverride)
	if err != nil {
		log.Errorf("[UpdateServerNodeConfig] OverrideModel Error: %v", err.Error())
		return fmt.Errorf("server node config is invalid: %w: %w", err, xerr.NewErrCodeMsg(xerr.InvalidParams, "server node config is invalid"))
	}

	if allInherited {
		err = nodeStore.DeleteServerConfigOverride(ctx, req.ServerID)
	} else {
		err = nodeStore.SaveServerConfigOverride(ctx, data)
	}
	if err != nil {
		log.Errorf("[UpdateServerNodeConfig] SaveServerConfigOverride Error: %v", err.Error())
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update server node config error: %v", err)
	}

	return nodeStore.ClearServerCache(ctx, req.ServerID)
}
