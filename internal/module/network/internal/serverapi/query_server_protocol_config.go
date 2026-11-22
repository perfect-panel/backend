package serverapi

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/nodeconfig"
	"github.com/perfect-panel/backend/internal/module/network/internal/protocolmap"
	"github.com/perfect-panel/backend/pkg/logger"
)

// QueryServerProtocolConfig returns the configuration a node pulls for its
// server: the enabled protocols, only the requested ones when the node names
// any, with the node settings in effect after the server's override.
func (s *Service) QueryServerProtocolConfig(ctx context.Context, req *dto.QueryServerConfigRequest) (*dto.QueryServerConfigResponse, error) {
	log := logger.WithContext(ctx)
	data, err := s.deps.Servers.FindOneServer(ctx, req.ServerID)
	if err != nil {
		log.Errorf("[QueryServerProtocolConfig] FindOneServer Error: %s", err.Error())
		return nil, err
	}

	dst, err := data.UnmarshalProtocols()
	if err != nil {
		log.Errorf("[QueryServerProtocolConfig] UnmarshalProtocols Error: %s", err.Error())
		return nil, err
	}
	protocols, err := protocolmap.ToDTO(node.SanitizeProtocolsForNodeDistribution(dst))
	if err != nil {
		return nil, err
	}

	// Only the enabled protocols are distributed to the node.
	var enabledProtocols []dto.Protocol
	for _, p := range protocols {
		if p.Enable {
			enabledProtocols = append(enabledProtocols, p)
		}
	}
	protocols = enabledProtocols

	if len(req.Protocols) > 0 {
		// A requested name matches in its stored form; one that does not
		// normalize is compared as given.
		var filtered []dto.Protocol
		protocolSet := make(map[string]struct{})
		for _, p := range req.Protocols {
			protocol, err := node.NormalizeProtocolForStorage(node.Protocol{Type: p})
			if err != nil {
				protocolSet[p] = struct{}{}
				continue
			}
			protocolSet[protocol.Type] = struct{}{}
		}
		for _, p := range protocols {
			if _, exists := protocolSet[p.Type]; exists {
				filtered = append(filtered, p)
			}
		}
		protocols = filtered
	}

	settings := s.deps.Config().Node
	nodeValues := nodeconfig.GlobalValues(settings)
	override, err := s.deps.Overrides.FindServerConfigOverride(ctx, req.ServerID)
	if err != nil {
		log.Errorf("[QueryServerProtocolConfig] FindServerConfigOverride Error: %s", err.Error())
		return nil, err
	}
	if override != nil {
		if err = nodeconfig.ApplyOverride(&nodeValues, override); err != nil {
			log.Errorf("[QueryServerProtocolConfig] ApplyOverride Error: %s", err.Error())
			return nil, err
		}
	}

	return &dto.QueryServerConfigResponse{
		TrafficReportThreshold: settings.TrafficReportThreshold,
		PushInterval:           settings.NodePushInterval,
		PullInterval:           settings.NodePullInterval,
		IPStrategy:             nodeValues.IPStrategy,
		DNS:                    nodeValues.DNS,
		Block:                  nodeValues.Block,
		Outbound:               nodeValues.Outbound,
		Protocols:              protocols,
		Total:                  int64(len(protocols)),
	}, nil
}
