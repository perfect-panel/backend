package adapter

import (
	"strings"

	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/pkg/logger"
)

type Adapter struct {
	Type           string            // 协议类型
	SiteName       string            // 站点名称
	Servers        []*node.Node      // 服务器列表
	UserInfo       User              // 用户信息
	ClientTemplate string            // 客户端配置模板
	OutputFormat   string            // 输出格式，默认是 base64
	SubscribeName  string            // 订阅名称
	Params         map[string]string // 其他参数
}

type Option func(*Adapter)

func WithParams(params map[string]string) Option {
	return func(opts *Adapter) {
		opts.Params = params
	}
}

// WithServers 设置服务器列表
func WithServers(servers []*node.Node) Option {
	return func(opts *Adapter) {
		opts.Servers = servers
	}
}

// WithUserInfo 设置用户信息
func WithUserInfo(user User) Option {
	return func(opts *Adapter) {
		opts.UserInfo = user
	}
}

// WithOutputFormat 设置输出格式
func WithOutputFormat(format string) Option {
	return func(opts *Adapter) {
		opts.OutputFormat = format
	}
}

// WithSiteName 设置站点名称
func WithSiteName(name string) Option {
	return func(opts *Adapter) {
		opts.SiteName = name
	}
}

// WithSubscribeName 设置订阅名称
func WithSubscribeName(name string) Option {
	return func(opts *Adapter) {
		opts.SubscribeName = name
	}
}

func NewAdapter(tpl string, opts ...Option) *Adapter {
	adapter := &Adapter{
		Servers:        []*node.Node{},
		UserInfo:       User{},
		ClientTemplate: tpl,
		OutputFormat:   "base64", // 默认输出格式
	}

	for _, opt := range opts {
		opt(adapter)
	}

	return adapter
}

func (adapter *Adapter) Client() (*Client, error) {
	client := &Client{
		SiteName:       adapter.SiteName,
		SubscribeName:  adapter.SubscribeName,
		ClientTemplate: adapter.ClientTemplate,
		OutputFormat:   adapter.OutputFormat,
		Proxies:        []Proxy{},
		UserInfo:       adapter.UserInfo,
		Params:         adapter.Params,
	}

	proxies, err := adapter.Proxies(adapter.Servers)
	if err != nil {
		return nil, err
	}
	client.Proxies = proxies
	return client, nil
}

func (adapter *Adapter) Proxies(servers []*node.Node) ([]Proxy, error) {
	var proxies []Proxy

	for _, item := range servers {
		if item.Server == nil {
			logger.Errorf("[Adapter] Server is nil for node ID: %d", item.Id)
			continue
		}
		protocols, err := item.Server.UnmarshalProtocols()
		if err != nil {
			logger.Errorf("[Adapter] Unmarshal Protocols error: %s; server id : %d", err.Error(), item.ServerId)
			continue
		}
		for _, protocol := range protocols {
			if protocol.Type == item.Protocol {
				proxies = append(
					proxies,
					Proxy{
						Sort:                    item.Sort,
						Name:                    item.Name,
						Server:                  item.Address,
						Port:                    item.Port,
						Type:                    item.Protocol,
						Tags:                    strings.Split(item.Tags, ","),
						Security:                protocol.Security,
						SNI:                     protocol.SNI,
						AllowInsecure:           protocol.AllowInsecure,
						Fingerprint:             protocol.Fingerprint,
						RealityServerAddr:       protocol.RealityServerAddr,
						RealityServerPort:       protocol.RealityServerPort,
						RealityPrivateKey:       protocol.RealityPrivateKey,
						RealityPublicKey:        protocol.RealityPublicKey,
						RealityShortId:          protocol.RealityShortId,
						Transport:               protocol.Transport,
						Host:                    protocol.Host,
						Path:                    protocol.Path,
						ServiceName:             protocol.ServiceName,
						Method:                  protocol.Cipher,
						ServerKey:               protocol.ServerKey,
						UoT:                     protocol.UoT,
						UoTVersion:              protocol.UoTVersion,
						Flow:                    protocol.Flow,
						HopPorts:                protocol.HopPorts,
						HopInterval:             protocol.HopInterval,
						ObfsPassword:            protocol.ObfsPassword,
						UpMbps:                  protocol.UpMbps,
						DownMbps:                protocol.DownMbps,
						DisableSNI:              protocol.DisableSNI,
						ReduceRtt:               protocol.ReduceRtt,
						UDPRelayMode:            protocol.UDPRelayMode,
						CongestionController:    protocol.CongestionController,
						PaddingScheme:           protocol.PaddingScheme,
						Multiplex:               protocol.Multiplex,
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
						CertDNSProvider:         protocol.CertDNSProvider,
						CertDNSEnv:              protocol.CertDNSEnv,
					},
				)
			}
		}
	}

	return proxies, nil
}
