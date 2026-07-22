package server

import (
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	serverv1 "github.com/perfect-panel/server/api/server/v1"
	"github.com/perfect-panel/server/internal/model/dto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func bindOnlineUsersRequest(ctx *app.RequestContext, req *dto.OnlineUsersRequest) error {
	if !isProtobufRequest(ctx) {
		return ctx.BindJSON(req)
	}

	var message serverv1.PushOnlineUsersRequest
	if err := ctx.BindProtobuf(&message); err != nil {
		return err
	}
	req.Users = make([]dto.OnlineUser, 0, len(message.Users))
	for _, user := range message.Users {
		if user == nil {
			continue
		}
		req.Users = append(req.Users, dto.OnlineUser{SID: user.UserId, IP: user.Ip})
	}
	return nil
}

func bindUserTrafficRequest(ctx *app.RequestContext, req *dto.ServerPushUserTrafficRequest) error {
	if !isProtobufRequest(ctx) {
		return ctx.BindJSON(req)
	}

	var message serverv1.PushUserTrafficRequest
	if err := ctx.BindProtobuf(&message); err != nil {
		return err
	}
	req.Traffic = make([]dto.UserTraffic, 0, len(message.Traffic))
	for _, traffic := range message.Traffic {
		if traffic == nil {
			continue
		}
		req.Traffic = append(req.Traffic, dto.UserTraffic{
			SID:      traffic.UserId,
			Upload:   traffic.Upload,
			Download: traffic.Download,
		})
	}
	return nil
}

func bindServerStatusRequest(ctx *app.RequestContext, req *dto.ServerPushStatusRequest) error {
	if !isProtobufRequest(ctx) {
		return ctx.BindJSON(req)
	}

	var message serverv1.PushServerStatusRequest
	if err := ctx.BindProtobuf(&message); err != nil {
		return err
	}
	req.Cpu = message.Cpu
	req.Mem = message.Mem
	req.Disk = message.Disk
	req.UpdatedAt = message.UpdatedAt
	return nil
}

func serverConfigResponseToProtobuf(response *dto.GetServerConfigResponse) (*serverv1.GetServerConfigResponse, error) {
	config, err := valueToStruct(response.Config)
	if err != nil {
		return nil, err
	}
	return &serverv1.GetServerConfigResponse{
		Code:    200,
		Message: "success",
		Data: &serverv1.ServerConfigData{
			Basic: &serverv1.ServerBasic{
				PushInterval: response.Basic.PushInterval,
				PullInterval: response.Basic.PullInterval,
			},
			Protocol: response.Protocol,
			Config:   config,
		},
	}, nil
}

func serverUserListResponseToProtobuf(response *dto.GetServerUserListResponse) *serverv1.GetServerUserListResponse {
	users := make([]*serverv1.ServerUser, 0, len(response.Users))
	for _, user := range response.Users {
		users = append(users, &serverv1.ServerUser{
			Id:          user.Id,
			Uuid:        user.UUID,
			SpeedLimit:  user.SpeedLimit,
			DeviceLimit: user.DeviceLimit,
		})
	}
	return &serverv1.GetServerUserListResponse{
		Code:    200,
		Message: "success",
		Data:    &serverv1.ServerUserListData{Users: users},
	}
}

func queryServerProtocolConfigResponseToProtobuf(response *dto.QueryServerConfigResponse) (*serverv1.QueryServerProtocolConfigResponse, error) {
	dns := make([]*serverv1.DNSResolver, 0, len(response.DNS))
	for _, resolver := range response.DNS {
		dns = append(dns, &serverv1.DNSResolver{
			Proto:      resolver.Proto,
			Address:    resolver.Address,
			ServerName: resolver.ServerName,
			Domains:    append([]string(nil), resolver.Domains...),
		})
	}

	outbound := make([]*serverv1.Outbound, 0, len(response.Outbound))
	for _, item := range response.Outbound {
		converted, err := outboundToProtobuf(item)
		if err != nil {
			return nil, err
		}
		outbound = append(outbound, converted)
	}

	protocols := make([]*serverv1.ServerProtocol, 0, len(response.Protocols))
	for _, protocol := range response.Protocols {
		converted, err := serverProtocolToProtobuf(protocol)
		if err != nil {
			return nil, err
		}
		protocols = append(protocols, converted)
	}

	return &serverv1.QueryServerProtocolConfigResponse{
		Code:    200,
		Message: "success",
		Data: &serverv1.QueryServerProtocolConfigData{
			TrafficReportThreshold: response.TrafficReportThreshold,
			PushInterval:           response.PushInterval,
			PullInterval:           response.PullInterval,
			IpStrategy:             response.IPStrategy,
			Dns:                    dns,
			Block:                  append([]string(nil), response.Block...),
			Outbound:               outbound,
			Protocols:              protocols,
			Total:                  response.Total,
		},
	}, nil
}

func outboundToProtobuf(outbound dto.NodeOutbound) (*serverv1.Outbound, error) {
	pluginOptions, err := valueToProtobufValue(outbound.PluginOptions)
	if err != nil {
		return nil, err
	}
	return &serverv1.Outbound{
		Name:                    outbound.Name,
		Protocol:                outbound.Protocol,
		Address:                 outbound.Address,
		Port:                    outbound.Port,
		User:                    outbound.User,
		Password:                outbound.Password,
		Uuid:                    outbound.UUID,
		Cipher:                  outbound.Cipher,
		Plugin:                  outbound.Plugin,
		PluginOptions:           pluginOptions,
		Security:                outbound.Security,
		Sni:                     outbound.SNI,
		Alpn:                    append([]string(nil), outbound.ALPN...),
		AllowInsecure:           outbound.AllowInsecure,
		Fingerprint:             outbound.Fingerprint,
		Transport:               outbound.Transport,
		Host:                    outbound.Host,
		Path:                    outbound.Path,
		ServiceName:             outbound.ServiceName,
		XhttpMode:               outbound.XHTTPMode,
		XhttpExtra:              outbound.XHTTPExtra,
		Flow:                    outbound.Flow,
		Encryption:              outbound.Encryption,
		EncryptionMode:          outbound.EncryptionMode,
		EncryptionRtt:           outbound.EncryptionRTT,
		EncryptionTicket:        outbound.EncryptionTicket,
		EncryptionClientPadding: outbound.EncryptionPadding,
		EncryptionPassword:      outbound.EncryptionPassword,
		Multiplex:               outbound.Multiplex,
		Uot:                     outbound.UoT,
		UotVersion:              int64(outbound.UoTVersion),
		CongestionController:    outbound.CongestionController,
		UdpStream:               outbound.UDPStream,
		ReduceRtt:               outbound.ReduceRtt,
		Heartbeat:               int64(outbound.Heartbeat),
		RealityPublicKey:        outbound.RealityPublicKey,
		RealityShortId:          outbound.RealityShortId,
		SpiderX:                 outbound.SpiderX,
		Settings:                outbound.Settings,
		StreamSettings:          outbound.StreamSettings,
		Rules:                   append([]string(nil), outbound.Rules...),
	}, nil
}

func serverProtocolToProtobuf(protocol dto.Protocol) (*serverv1.ServerProtocol, error) {
	pluginOptions, err := valueToProtobufValue(protocol.PluginOptions)
	if err != nil {
		return nil, err
	}
	return &serverv1.ServerProtocol{
		Type:                    protocol.Type,
		Port:                    uint32(protocol.Port),
		Version:                 int64(protocol.Version),
		Mode:                    protocol.Mode,
		Enable:                  protocol.Enable,
		Security:                protocol.Security,
		Network:                 protocol.Network,
		Sni:                     protocol.SNI,
		Alpn:                    append([]string(nil), protocol.ALPN...),
		AllowInsecure:           protocol.AllowInsecure,
		Fingerprint:             protocol.Fingerprint,
		RealityServerAddr:       protocol.RealityServerAddr,
		RealityServerPort:       int64(protocol.RealityServerPort),
		RealityPrivateKey:       protocol.RealityPrivateKey,
		RealityPublicKey:        protocol.RealityPublicKey,
		RealityShortId:          protocol.RealityShortId,
		Transport:               protocol.Transport,
		Host:                    protocol.Host,
		Path:                    protocol.Path,
		ServiceName:             protocol.ServiceName,
		Cipher:                  protocol.Cipher,
		ServerKey:               protocol.ServerKey,
		Plugin:                  protocol.Plugin,
		PluginOptions:           pluginOptions,
		Flow:                    protocol.Flow,
		Uot:                     protocol.UoT,
		UotVersion:              int64(protocol.UoTVersion),
		AcceptProxyProtocol:     protocol.AcceptProxyProtocol,
		HopPorts:                protocol.HopPorts,
		HopInterval:             int64(protocol.HopInterval),
		ObfsPassword:            protocol.ObfsPassword,
		DisableSni:              protocol.DisableSNI,
		ReduceRtt:               protocol.ReduceRtt,
		Heartbeat:               int64(protocol.Heartbeat),
		UdpRelayMode:            protocol.UDPRelayMode,
		CongestionController:    protocol.CongestionController,
		QuicCongestionControl:   protocol.QUICCongestionControl,
		Multiplex:               protocol.Multiplex,
		PaddingScheme:           protocol.PaddingScheme,
		TrafficPattern:          protocol.TrafficPattern,
		UserHintIsMandatory:     protocol.UserHintIsMandatory,
		UpMbps:                  int64(protocol.UpMbps),
		DownMbps:                int64(protocol.DownMbps),
		Obfs:                    protocol.Obfs,
		SsrProtocol:             protocol.SSRProtocol,
		ProtocolParam:           protocol.ProtocolParam,
		ObfsParam:               protocol.ObfsParam,
		ObfsHost:                protocol.ObfsHost,
		ObfsPath:                protocol.ObfsPath,
		XhttpMode:               protocol.XhttpMode,
		XhttpExtra:              protocol.XhttpExtra,
		Encryption:              protocol.Encryption,
		EncryptionMode:          protocol.EncryptionMode,
		EncryptionRtt:           protocol.EncryptionRtt,
		EncryptionTicket:        protocol.EncryptionTicket,
		EncryptionServerPadding: protocol.EncryptionServerPadding,
		EncryptionPrivateKey:    protocol.EncryptionPrivateKey,
		EncryptionClientPadding: protocol.EncryptionClientPadding,
		EncryptionPassword:      protocol.EncryptionPassword,
		EchEnable:               protocol.EchEnable,
		EchServerName:           protocol.EchServerName,
		Ratio:                   protocol.Ratio,
		CertMode:                protocol.CertMode,
		CertDnsProvider:         protocol.CertDNSProvider,
		CertDnsEnv:              protocol.CertDNSEnv,
	}, nil
}

func valueToStruct(value interface{}) (*structpb.Struct, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := &structpb.Struct{}
	if err := protojson.Unmarshal(body, result); err != nil {
		return nil, err
	}
	return result, nil
}

func valueToProtobufValue(value interface{}) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := &structpb.Value{}
	if err := protojson.Unmarshal(body, result); err != nil {
		return nil, err
	}
	return result, nil
}
