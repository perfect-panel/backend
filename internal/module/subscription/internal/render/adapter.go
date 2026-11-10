// Package render builds the client configurations of subscription delivery
// and template previews: it maps nodes to the proxies a client template
// renders and executes the template in the application's output format.
package render

import (
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/pkg/logger"
)

// Adapter collects what a client configuration is rendered from.
type Adapter struct {
	Type           string            // Protocol type
	SiteName       string            // Site name
	Servers        []*node.Node      // Nodes the configuration lists
	UserInfo       User              // Subscriber the configuration is for
	ClientTemplate string            // Client configuration template
	OutputFormat   string            // Output format, base64 by default
	SubscribeName  string            // Subscription (plan) name
	Params         map[string]string // Template parameters
}

type Option func(*Adapter)

func WithParams(params map[string]string) Option {
	return func(opts *Adapter) {
		opts.Params = params
	}
}

// WithServers sets the nodes the configuration lists.
func WithServers(servers []*node.Node) Option {
	return func(opts *Adapter) {
		opts.Servers = servers
	}
}

// WithUserInfo sets the subscriber the configuration is for.
func WithUserInfo(user User) Option {
	return func(opts *Adapter) {
		opts.UserInfo = user
	}
}

// WithOutputFormat sets the output format.
func WithOutputFormat(format string) Option {
	return func(opts *Adapter) {
		opts.OutputFormat = format
	}
}

// WithSiteName sets the site name.
func WithSiteName(name string) Option {
	return func(opts *Adapter) {
		opts.SiteName = name
	}
}

// WithSubscribeName sets the subscription (plan) name.
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
		OutputFormat:   "base64",
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
		itemProtocol := canonicalProtocolType(item.Protocol)
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
			if protocolType := canonicalProtocolType(protocol.Type); protocolType == itemProtocol {
				protocol.Type = protocolType
				if protocolType == "nowhere" {
					protocol, err = node.NormalizeProtocolForStorage(protocol)
					if err != nil {
						return nil, fmt.Errorf("nowhere node %d configuration: %w", item.Id, err)
					}
					if !protocol.Enable {
						continue
					}
				}
				proxies = append(proxies, newProxy(item, protocol))
			}
		}
	}

	return proxies, nil
}

// newProxy is the one mapping from a node and its server protocol to the
// proxy a client template renders. It lists the client-facing settings one by
// one on purpose: the protocol also holds server secrets and server-side
// targets (the REALITY private key and handshake target, the VLESS
// encryption ticket, padding and private key, the DNS provider and its
// credentials for the certificate) that must never reach a subscriber. Every
// field of the proxy is handed to the admin-authored template, toJson and
// toYaml included, so leaving a setting out here is the only thing that keeps
// it from every subscriber. TestNewProxyMapsEveryClientField keeps the list
// complete and TestTemplatesCannotReachServerOnlySettings keeps it tight.
func newProxy(item *node.Node, protocol node.Protocol) Proxy {
	plugin, pluginOptions := clientPluginConfig(protocol, item.Address)
	return Proxy{
		Sort:     item.Sort,
		Name:     item.Name,
		Server:   item.Address,
		Port:     item.Port,
		Type:     protocol.Type,
		Tags:     strings.Split(item.Tags, ","),
		Version:  protocol.Version,
		Mode:     protocol.Mode,
		Network:  protocol.Network,
		Security: protocol.Security,
		SNI:      protocol.SNI,
		ALPN:     protocol.ALPN,
		// Certificate pinning and skipping verification exclude each other:
		// a client that skips verification makes the pin meaningless.
		AllowInsecure:           protocol.AllowInsecure && protocol.CertPinSHA256 == "",
		Fingerprint:             protocol.Fingerprint,
		RealityPublicKey:        protocol.RealityPublicKey,
		RealityShortId:          protocol.RealityShortId,
		Transport:               protocol.Transport,
		Host:                    protocol.Host,
		Path:                    protocol.Path,
		ServiceName:             protocol.ServiceName,
		Method:                  protocol.Cipher,
		ServerKey:               protocol.ServerKey,
		Plugin:                  plugin,
		PluginOptions:           pluginOptions,
		UoT:                     protocol.UoT,
		UoTVersion:              protocol.UoTVersion,
		AcceptProxyProtocol:     protocol.AcceptProxyProtocol,
		Flow:                    protocol.Flow,
		HopPorts:                protocol.HopPorts,
		HopInterval:             protocol.HopInterval,
		ObfsPassword:            protocol.ObfsPassword,
		UpMbps:                  protocol.UpMbps,
		DownMbps:                protocol.DownMbps,
		DisableSNI:              protocol.DisableSNI,
		ReduceRtt:               protocol.ReduceRtt,
		Heartbeat:               protocol.Heartbeat,
		UDPRelayMode:            protocol.UDPRelayMode,
		CongestionController:    protocol.CongestionController,
		QUICCongestionControl:   protocol.QUICCongestionControl,
		PaddingScheme:           protocol.PaddingScheme,
		Multiplex:               protocol.Multiplex,
		TrafficPattern:          protocol.TrafficPattern,
		UserHintIsMandatory:     protocol.UserHintIsMandatory,
		Obfs:                    protocol.Obfs,
		SSRProtocol:             protocol.SSRProtocol,
		ProtocolParam:           protocol.ProtocolParam,
		ObfsParam:               protocol.ObfsParam,
		ObfsHost:                protocol.ObfsHost,
		ObfsPath:                protocol.ObfsPath,
		XhttpMode:               protocol.XhttpMode,
		XhttpExtra:              protocol.XhttpExtra,
		Encryption:              protocol.Encryption,
		EncryptionMode:          protocol.EncryptionMode,
		EncryptionRtt:           protocol.EncryptionRtt,
		EncryptionClientPadding: protocol.EncryptionClientPadding,
		EncryptionPassword:      protocol.EncryptionPassword,
		EchEnable:               protocol.EchEnable,
		EchServerName:           protocol.EchServerName,
		Ratio:                   protocol.Ratio,
		CertMode:                protocol.CertMode,
		CertPinSHA256:           protocol.CertPinSHA256,
	}
}

func canonicalProtocolType(raw string) string {
	protocol, err := node.NormalizeProtocolForStorage(node.Protocol{Type: raw})
	if err != nil {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return protocol.Type
}
