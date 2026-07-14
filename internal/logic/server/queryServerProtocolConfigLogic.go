package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/nodeconfig"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type QueryServerProtocolConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryServerProtocolConfigLogic Get Server Protocol Config
func NewQueryServerProtocolConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryServerProtocolConfigLogic {
	return &QueryServerProtocolConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryServerProtocolConfigLogic) QueryServerProtocolConfig(req *dto.QueryServerConfigRequest) (resp *dto.QueryServerConfigResponse, err error) {
	// find server
	data, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerID)
	if err != nil {
		l.Errorf("[GetServerProtocols] FindOneServer Error: %s", err.Error())
		return nil, err
	}

	// handler protocols
	var protocols []dto.Protocol
	dst, err := data.UnmarshalProtocols()
	if err != nil {
		l.Errorf("[FilterServerList] UnmarshalProtocols Error: %s", err.Error())
		return nil, err
	}
	tool.DeepCopy(&protocols, dst)

	// only return enabled protocols for node distribution
	var enabledProtocols []dto.Protocol
	for _, p := range protocols {
		if p.Enable {
			enabledProtocols = append(enabledProtocols, p)
		}
	}
	protocols = enabledProtocols

	// filter by req.Protocols

	if len(req.Protocols) > 0 {
		var filtered []dto.Protocol
		protocolSet := make(map[string]struct{})
		for _, p := range req.Protocols {
			protocolSet[p] = struct{}{}
		}
		for _, p := range protocols {
			if _, exists := protocolSet[p.Type]; exists {
				filtered = append(filtered, p)
			}
		}
		protocols = filtered
	}

	nodeValues := nodeconfig.GlobalValues(l.svcCtx.Config.Node)
	override, err := l.svcCtx.Store.Node().FindServerConfigOverride(l.ctx, req.ServerID)
	if err != nil {
		l.Errorf("[GetServerProtocols] FindServerConfigOverride Error: %s", err.Error())
		return nil, err
	}
	if override != nil {
		if err = nodeconfig.ApplyOverride(&nodeValues, override); err != nil {
			l.Errorf("[GetServerProtocols] ApplyOverride Error: %s", err.Error())
			return nil, err
		}
	}

	return &dto.QueryServerConfigResponse{
		TrafficReportThreshold: l.svcCtx.Config.Node.TrafficReportThreshold,
		PushInterval:           l.svcCtx.Config.Node.NodePushInterval,
		PullInterval:           l.svcCtx.Config.Node.NodePullInterval,
		IPStrategy:             nodeValues.IPStrategy,
		DNS:                    nodeValues.DNS,
		Block:                  nodeValues.Block,
		Outbound:               nodeValues.Outbound,
		Protocols:              protocols,
		Total:                  int64(len(protocols)),
	}, nil
}
