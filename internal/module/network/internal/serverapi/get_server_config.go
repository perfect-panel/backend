package serverapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetServerConfig returns a server protocol's configuration in the legacy
// shape, from the server's response cache when it holds one. meta carries
// the node's If-None-Match: a matching ETag returns xerr.ErrNotModified. The
// returned response metadata holds the headers set before any error.
func (s *Service) GetServerConfig(ctx context.Context, req *dto.GetServerConfigRequest, meta RequestMeta) (*dto.GetServerConfigResponse, ResponseMeta, error) {
	log := logger.WithContext(ctx)
	response := NewResponseMeta()
	cacheKey := fmt.Sprintf("%s%d:%s", node.ServerConfigCacheKey, req.ServerId, req.Protocol)
	if cache, err := s.deps.Redis.Get(ctx, cacheKey).Result(); err == nil && cache != "" {
		etag := httpx.GenerateETag([]byte(cache))
		if meta.IfNoneMatch == etag {
			return nil, response, xerr.ErrNotModified
		}
		response.SetHeader("ETag", etag)
		resp := &dto.GetServerConfigResponse{}
		if err := json.Unmarshal([]byte(cache), resp); err != nil {
			log.Errorw("[ServerConfigCacheKey] json unmarshal error", logger.Field("error", err.Error()))
			return nil, response, err
		}
		return resp, response, nil
	}
	generation, err := s.deps.Caches.ServerCacheGeneration(ctx, req.ServerId)
	if err != nil {
		return nil, response, err
	}
	data, err := s.deps.Servers.FindOneServer(ctx, req.ServerId)
	if err != nil {
		log.Errorw("[GetServerConfig] FindOne error", logger.Field("error", err.Error()))
		return nil, response, err
	}

	// Older nodes still ask for hysteria by its former name, hysteria2.
	protocolRequest := req.Protocol
	if protocolRequest == Hysteria2 {
		protocolRequest = Hysteria
	}

	protocols, err := data.UnmarshalProtocols()
	if err != nil {
		return nil, response, err
	}
	var cfg map[string]any
	matched := false
	for _, protocol := range protocols {
		if protocol.Enable && protocol.Type == protocolRequest {
			matched = true
			cfg = compatible(protocol)
			break
		}
	}

	if cfg == nil {
		if matched {
			return nil, response, fmt.Errorf("protocol %s is not supported by the legacy server config endpoint; use /v2/server/{server_id}", req.Protocol)
		}
		return nil, response, fmt.Errorf("protocol %s not found or disabled", req.Protocol)
	}

	settings := s.deps.Config().Node
	resp := &dto.GetServerConfigResponse{
		Basic: dto.ServerBasic{
			PullInterval: settings.NodePullInterval,
			PushInterval: settings.NodePushInterval,
		},
		Protocol: req.Protocol,
		Config:   cfg,
	}
	c, err := json.Marshal(resp)
	if err != nil {
		log.Errorw("[GetServerConfig] json marshal error", logger.Field("error", err.Error()))
		return nil, response, err
	}
	etag := httpx.GenerateETag(c)
	response.SetHeader("ETag", etag)
	if err := s.deps.Caches.SetServerCache(ctx, req.ServerId, cacheKey, c, generation); err != nil {
		log.Errorw("[GetServerConfig] cache set error", logger.Field("error", err.Error()))
	}
	if meta.IfNoneMatch == etag {
		return nil, response, xerr.ErrNotModified
	}

	return resp, response, nil
}

// compatible renders a protocol in the legacy server-config shape, or nil for
// a protocol that shape cannot express.
func compatible(config node.Protocol) map[string]any {
	var result any
	switch config.Type {
	case ShadowSocks:
		result = ShadowsocksNode{
			Port:      config.Port,
			Cipher:    config.Cipher,
			ServerKey: base64.StdEncoding.EncodeToString([]byte(config.ServerKey)),
		}
	case Vless:
		result = VlessNode{
			Port:            config.Port,
			Flow:            config.Flow,
			Network:         config.Transport,
			TransportConfig: legacyTransportConfig(config),
			Security:        config.Security,
			SecurityConfig:  legacySecurityConfig(config),
		}
	case Vmess:
		result = VmessNode{
			Port:            config.Port,
			Network:         config.Transport,
			TransportConfig: legacyTransportConfig(config),
			Security:        config.Security,
			SecurityConfig:  legacySecurityConfig(config),
		}
	case Trojan:
		result = TrojanNode{
			Port:            config.Port,
			Network:         config.Transport,
			TransportConfig: legacyTransportConfig(config),
			Security:        config.Security,
			SecurityConfig:  legacySecurityConfig(config),
		}
	case AnyTLS:
		security := legacySecurityConfig(config)
		security.PaddingScheme = config.PaddingScheme
		result = AnyTLSNode{
			Port:           config.Port,
			SecurityConfig: security,
		}
	case Tuic:
		result = TuicNode{
			Port:           config.Port,
			SecurityConfig: legacySecurityConfig(config),
		}
	case Hysteria:
		result = Hysteria2Node{
			Port:           config.Port,
			HopPorts:       config.HopPorts,
			HopInterval:    config.HopInterval,
			ObfsPassword:   config.ObfsPassword,
			SecurityConfig: legacySecurityConfig(config),
		}

	}
	var resp map[string]any
	s, _ := json.Marshal(result)
	_ = json.Unmarshal(s, &resp)
	return resp
}

// legacySecurityConfig is the TLS/REALITY block every legacy protocol shape
// shares. Only AnyTLS adds its padding scheme.
func legacySecurityConfig(config node.Protocol) *SecurityConfig {
	return &SecurityConfig{
		SNI:                  config.SNI,
		AllowInsecure:        &config.AllowInsecure,
		Fingerprint:          config.Fingerprint,
		RealityServerAddress: config.RealityServerAddr,
		RealityServerPort:    config.RealityServerPort,
		RealityPrivateKey:    config.RealityPrivateKey,
		RealityPublicKey:     config.RealityPublicKey,
		RealityShortId:       config.RealityShortId,
	}
}

// legacyTransportConfig is the transport block of the legacy VLESS, VMess
// and Trojan shapes.
func legacyTransportConfig(config node.Protocol) *TransportConfig {
	return &TransportConfig{
		Path:                 config.Path,
		Host:                 config.Host,
		ServiceName:          config.ServiceName,
		DisableSNI:           config.DisableSNI,
		ReduceRtt:            config.ReduceRtt,
		UDPRelayMode:         config.UDPRelayMode,
		CongestionController: config.CongestionController,
	}
}
