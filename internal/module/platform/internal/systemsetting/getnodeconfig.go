package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetNodeConfig returns the stored node settings, the outbound credentials
// masked. A malformed DNS or outbound document is reported as an error
// instead of taking the process down.
func (s *Service) GetNodeConfig(ctx context.Context) (*dto.NodeConfig, error) {
	view, err := s.storedNodeConfig(ctx)
	if err != nil {
		return nil, err
	}
	maskOutboundSecrets(view.Outbound)
	return view, nil
}

// storedNodeConfig reads the node settings as stored, credentials in clear.
func (s *Service) storedNodeConfig(ctx context.Context) (*dto.NodeConfig, error) {
	rows, err := s.deps.System.GetNodeConfig(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get node config: %v", err)
	}
	parsed, err := system.ParseNodeConfig(rows)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "parse node config: %v", err)
	}
	return nodeConfigView(parsed), nil
}

// nodeConfigView converts the parsed configuration to its admin view; the
// snapshot types mirror the configuration types field for field. NodeSecret
// is deliberately shown in clear: it is the credential administrators copy
// into every node's configuration to deploy a node, and there is no other
// way to read it.
func nodeConfigView(c config.NodeConfig) *dto.NodeConfig {
	view := &dto.NodeConfig{
		NodeSecret:             c.NodeSecret,
		NodePullInterval:       c.NodePullInterval,
		NodePushInterval:       c.NodePushInterval,
		TrafficReportThreshold: c.TrafficReportThreshold,
		IPStrategy:             c.IPStrategy,
		Block:                  c.Block,
	}
	if c.DNS != nil {
		view.DNS = make([]dto.PlatformNodeDNSSnapshot, len(c.DNS))
		for i, dns := range c.DNS {
			view.DNS[i] = dto.PlatformNodeDNSSnapshot(dns)
		}
	}
	if c.Outbound != nil {
		view.Outbound = make([]dto.PlatformNodeOutboundSnapshot, len(c.Outbound))
		for i, outbound := range c.Outbound {
			view.Outbound[i] = dto.PlatformNodeOutboundSnapshot(outbound)
		}
	}
	return view
}
