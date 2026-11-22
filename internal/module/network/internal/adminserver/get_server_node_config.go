package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/internal/nodeconfig"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetServerNodeConfig returns a server's node configuration: the global
// values, the server's override and the values in effect with it.
func (s *Service) GetServerNodeConfig(ctx context.Context, req *dto.GetServerNodeConfigRequest) (*dto.GetServerNodeConfigResponse, error) {
	log := logger.WithContext(ctx)
	nodeStore := s.deps.Store.Node()
	if _, err := nodeStore.FindOneServer(ctx, req.ServerID); err != nil {
		log.Errorf("[GetServerNodeConfig] FindOneServer Error: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find server error: %v", err)
	}

	override, err := nodeStore.FindServerConfigOverride(ctx, req.ServerID)
	if err != nil {
		log.Errorf("[GetServerNodeConfig] FindServerConfigOverride Error: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find server node config error: %v", err)
	}

	global := nodeconfig.GlobalValues(s.deps.Config().Node)
	effective := nodeconfig.CloneValues(global)
	if err := nodeconfig.ApplyOverride(&effective, override); err != nil {
		log.Errorf("[GetServerNodeConfig] ApplyOverride Error: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.ERROR, "apply server node config override error: %v", err)
	}
	overrideResp, err := nodeconfig.OverrideResponse(override)
	if err != nil {
		log.Errorf("[GetServerNodeConfig] OverrideResponse Error: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.ERROR, "parse server node config override error: %v", err)
	}

	return &dto.GetServerNodeConfigResponse{
		Global:    global,
		Override:  overrideResp,
		Effective: effective,
	}, nil
}
