package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetServerConfigLogic struct {
	logger.Logger
	ctx      context.Context
	svcCtx   *svc.ServiceContext
	request  RequestMeta
	response ResponseMeta
}

// NewGetServerConfigLogic Get server config
func NewGetServerConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext, request RequestMeta) *GetServerConfigLogic {
	return &GetServerConfigLogic{
		Logger:   logger.WithContext(ctx),
		ctx:      ctx,
		svcCtx:   svcCtx,
		request:  request,
		response: NewResponseMeta(),
	}
}

func (l *GetServerConfigLogic) ResponseMeta() ResponseMeta {
	return l.response
}

func (l *GetServerConfigLogic) GetServerConfig(req *dto.GetServerConfigRequest) (resp *dto.GetServerConfigResponse, err error) {
	cacheKey := fmt.Sprintf("%s%d:%s", node.ServerConfigCacheKey, req.ServerId, req.Protocol)
	cache, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if err == nil {
		if cache != "" {
			etag := tool.GenerateETag([]byte(cache))
			//  Check If-None-Match header
			match := l.request.IfNoneMatch
			if match == etag {
				return nil, xerr.StatusNotModified
			}
			l.response.SetHeader("ETag", etag)
			resp = &dto.GetServerConfigResponse{}
			err = json.Unmarshal([]byte(cache), resp)
			if err != nil {
				l.Errorw("[ServerConfigCacheKey] json unmarshal error", logger.Field("error", err.Error()))
				return nil, err
			}
			return resp, nil
		}
	}
	generation, err := l.svcCtx.Store.Node().ServerCacheGeneration(l.ctx, req.ServerId)
	if err != nil {
		return nil, err
	}
	data, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		l.Errorw("[GetServerConfig] FindOne error", logger.Field("error", err.Error()))
		return nil, err
	}

	// compatible hysteria2, remove in future versions
	protocolRequest := req.Protocol
	if protocolRequest == Hysteria2 {
		protocolRequest = Hysteria
	}

	protocols, err := data.UnmarshalProtocols()
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	for _, protocol := range protocols {
		if protocol.Enable && protocol.Type == protocolRequest {
			cfg = l.compatible(protocol)
			break
		}
	}

	if cfg == nil {
		return nil, fmt.Errorf("protocol %s not found or disabled", req.Protocol)
	}

	resp = &dto.GetServerConfigResponse{
		Basic: dto.ServerBasic{
			PullInterval: l.svcCtx.Config.Node.NodePullInterval,
			PushInterval: l.svcCtx.Config.Node.NodePushInterval,
		},
		Protocol: req.Protocol,
		Config:   cfg,
	}
	c, err := json.Marshal(resp)
	if err != nil {
		l.Errorw("[GetServerConfig] json marshal error", logger.Field("error", err.Error()))
		return nil, err
	}
	etag := tool.GenerateETag(c)
	l.response.SetHeader("ETag", etag)
	if err = l.svcCtx.Store.Node().SetServerCache(l.ctx, req.ServerId, cacheKey, c, generation); err != nil {
		l.Errorw("[GetServerConfig] cache set error", logger.Field("error", err.Error()))
	}
	//  Check If-None-Match header
	match := l.request.IfNoneMatch
	if match == etag {
		return nil, xerr.StatusNotModified
	}

	return resp, nil
}

func (l *GetServerConfigLogic) compatible(config node.Protocol) map[string]interface{} {
	var result interface{}
	switch config.Type {
	case ShadowSocks:
		result = ShadowsocksNode{
			Port:      config.Port,
			Cipher:    config.Cipher,
			ServerKey: base64.StdEncoding.EncodeToString([]byte(config.ServerKey)),
		}
	case Vless:
		result = VlessNode{
			Port:    config.Port,
			Flow:    config.Flow,
			Network: config.Transport,
			TransportConfig: &TransportConfig{
				Path:                 config.Path,
				Host:                 config.Host,
				ServiceName:          config.ServiceName,
				DisableSNI:           config.DisableSNI,
				ReduceRtt:            config.ReduceRtt,
				UDPRelayMode:         config.UDPRelayMode,
				CongestionController: config.CongestionController,
			},
			Security: config.Security,
			SecurityConfig: &SecurityConfig{
				SNI:                  config.SNI,
				AllowInsecure:        &config.AllowInsecure,
				Fingerprint:          config.Fingerprint,
				RealityServerAddress: config.RealityServerAddr,
				RealityServerPort:    config.RealityServerPort,
				RealityPrivateKey:    config.RealityPrivateKey,
				RealityPublicKey:     config.RealityPublicKey,
				RealityShortId:       config.RealityShortId,
			},
		}
	case Vmess:
		result = VmessNode{
			Port:    config.Port,
			Network: config.Transport,
			TransportConfig: &TransportConfig{
				Path:                 config.Path,
				Host:                 config.Host,
				ServiceName:          config.ServiceName,
				DisableSNI:           config.DisableSNI,
				ReduceRtt:            config.ReduceRtt,
				UDPRelayMode:         config.UDPRelayMode,
				CongestionController: config.CongestionController,
			},
			Security: config.Security,
			SecurityConfig: &SecurityConfig{
				SNI:                  config.SNI,
				AllowInsecure:        &config.AllowInsecure,
				Fingerprint:          config.Fingerprint,
				RealityServerAddress: config.RealityServerAddr,
				RealityServerPort:    config.RealityServerPort,
				RealityPrivateKey:    config.RealityPrivateKey,
				RealityPublicKey:     config.RealityPublicKey,
				RealityShortId:       config.RealityShortId,
			},
		}
	case Trojan:
		result = TrojanNode{
			Port:    config.Port,
			Network: config.Transport,
			TransportConfig: &TransportConfig{
				Path:                 config.Path,
				Host:                 config.Host,
				ServiceName:          config.ServiceName,
				DisableSNI:           config.DisableSNI,
				ReduceRtt:            config.ReduceRtt,
				UDPRelayMode:         config.UDPRelayMode,
				CongestionController: config.CongestionController,
			},
			Security: config.Security,
			SecurityConfig: &SecurityConfig{
				SNI:                  config.SNI,
				AllowInsecure:        &config.AllowInsecure,
				Fingerprint:          config.Fingerprint,
				RealityServerAddress: config.RealityServerAddr,
				RealityServerPort:    config.RealityServerPort,
				RealityPrivateKey:    config.RealityPrivateKey,
				RealityPublicKey:     config.RealityPublicKey,
				RealityShortId:       config.RealityShortId,
			},
		}
	case AnyTLS:
		result = AnyTLSNode{
			Port: config.Port,
			SecurityConfig: &SecurityConfig{
				SNI:                  config.SNI,
				AllowInsecure:        &config.AllowInsecure,
				Fingerprint:          config.Fingerprint,
				RealityServerAddress: config.RealityServerAddr,
				RealityServerPort:    config.RealityServerPort,
				RealityPrivateKey:    config.RealityPrivateKey,
				RealityPublicKey:     config.RealityPublicKey,
				RealityShortId:       config.RealityShortId,
				PaddingScheme:        config.PaddingScheme,
			},
		}
	case Tuic:
		result = TuicNode{
			Port: config.Port,
			SecurityConfig: &SecurityConfig{
				SNI:                  config.SNI,
				AllowInsecure:        &config.AllowInsecure,
				Fingerprint:          config.Fingerprint,
				RealityServerAddress: config.RealityServerAddr,
				RealityServerPort:    config.RealityServerPort,
				RealityPrivateKey:    config.RealityPrivateKey,
				RealityPublicKey:     config.RealityPublicKey,
				RealityShortId:       config.RealityShortId,
			},
		}
	case Hysteria:
		result = Hysteria2Node{
			Port:         config.Port,
			HopPorts:     config.HopPorts,
			HopInterval:  config.HopInterval,
			ObfsPassword: config.ObfsPassword,
			SecurityConfig: &SecurityConfig{
				SNI:                  config.SNI,
				AllowInsecure:        &config.AllowInsecure,
				Fingerprint:          config.Fingerprint,
				RealityServerAddress: config.RealityServerAddr,
				RealityServerPort:    config.RealityServerPort,
				RealityPrivateKey:    config.RealityPrivateKey,
				RealityPublicKey:     config.RealityPublicKey,
				RealityShortId:       config.RealityShortId,
			},
		}

	}
	var resp map[string]interface{}
	s, _ := json.Marshal(result)
	_ = json.Unmarshal(s, &resp)
	return resp
}
