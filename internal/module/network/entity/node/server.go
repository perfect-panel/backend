package node

import (
	"encoding/json"
	"time"

	"errors"

	"github.com/perfect-panel/server/pkg/logger"
	"gorm.io/gorm"
)

// Server is a proxy server: its address and location, and the protocols it
// offers, stored as JSON.
type Server struct {
	Id             int64      `gorm:"primary_key"`
	Name           string     `gorm:"type:varchar(100);not null;default:'';comment:Server Name"`
	Country        string     `gorm:"type:varchar(128);not null;default:'';comment:Country"`
	City           string     `gorm:"type:varchar(128);not null;default:'';comment:City"`
	Address        string     `gorm:"type:varchar(100);not null;default:'';comment:Server Address"`
	Sort           int        `gorm:"type:int;not null;default:0;comment:Sort"`
	Protocols      string     `gorm:"type:text;default:null;comment:Protocol"`
	LastReportedAt *time.Time `gorm:"comment:Last Reported Time"`
	CreatedAt      time.Time  `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt      time.Time  `gorm:"comment:Update Time"`
}

func (*Server) TableName() string {
	return "servers"
}

func (m *Server) BeforeCreate(tx *gorm.DB) error {
	if m.Sort == 0 {
		var maxSort int
		if err := tx.Model(&Server{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		m.Sort = maxSort + 1
	}
	return nil
}

func (m *Server) BeforeDelete(tx *gorm.DB) error {
	if err := tx.Exec("UPDATE servers SET sort = sort - 1 WHERE sort > ?", m.Sort).Error; err != nil {
		return err
	}
	return nil
}

func (m *Server) BeforeUpdate(tx *gorm.DB) error {
	var count int64
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Model(&Server{}).
		Where("sort = ? AND id != ?", m.Sort, m.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 1 {
		// The positions collide: renumber them all and move this server last.
		if err := reorderSortWithServer(tx); err != nil {
			logger.Errorf("[Server] BeforeUpdate reorderSort error: %v", err.Error())
			return err
		}
		var maxSort int
		if err := tx.Model(&Server{}).Select("MAX(sort)").Scan(&maxSort).Error; err != nil {
			return err
		}
		m.Sort = maxSort + 1
	}
	return nil
}

// MarshalProtocols stores list as the server's protocols. Every protocol
// needs a type, and no type may appear twice.
func (m *Server) MarshalProtocols(list []Protocol) error {
	var validate = make(map[string]bool)
	for _, protocol := range list {
		if protocol.Type == "" {
			return errors.New("protocol type is required")
		}
		if _, exists := validate[protocol.Type]; exists {
			return errors.New("duplicate protocol type: " + protocol.Type)
		}
		validate[protocol.Type] = true
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	m.Protocols = string(data)
	return nil
}

// UnmarshalProtocols returns the server's stored protocols; an empty value
// holds none.
func (m *Server) UnmarshalProtocols() ([]Protocol, error) {
	var list []Protocol
	if m.Protocols == "" {
		return list, nil
	}
	err := json.Unmarshal([]byte(m.Protocols), &list)
	if err != nil {
		return nil, err
	}
	for index := range list {
		upgradeStoredNowhere(&list[index])
	}
	return list, nil
}

// Protocol is one protocol a server offers, as stored in the server's
// protocols JSON.
type Protocol struct {
	// Common: the protocol type, such as shadowsocks, vless, vmess, hysteria2, tuic or nowhere.
	Type string `json:"type"`
	// Common: the inbound listening port.
	Port uint16 `json:"port"`
	// Version: Snell accepts 5 or 6, TUIC 5 and Nowhere 3; 0 means the protocol's default.
	Version int `json:"version,omitempty"`
	// Snell only: the Snell v6 mode; other protocols leave it unset.
	Mode string `json:"mode,omitempty"`
	// Common: whether the inbound protocol is enabled.
	Enable bool `json:"enable"`
	// TLS/REALITY protocols: none, tls or reality; each protocol limits the values it accepts.
	Security string `json:"security,omitempty"`
	// Listening network: tcp, udp or both; Nowhere normalizes it to mix, tcp or udp, and the other protocols check it against what each supports.
	Network string `json:"network,omitempty"`
	// TLS protocols: the certificate domain and the TLS server name; REALITY also uses it as the server name.
	SNI string `json:"sni,omitempty"`
	// TLS/HTTP/QUIC protocols: the TLS ALPN list; Nowhere requires exactly nw3.
	ALPN []string `json:"alpn,omitempty"`
	// TLS client compatibility: allows skipping certificate verification; inbound configurations usually ignore it, and it is not a server certificate setting.
	AllowInsecure bool `json:"allow_insecure,omitempty"`
	// TLS client compatibility: the uTLS fingerprint; node inbounds currently ignore it, it is kept only for outbound and legacy configurations.
	Fingerprint string `json:"fingerprint,omitempty"`
	// VLESS/VMess REALITY only: the address the REALITY handshake is forwarded to.
	RealityServerAddr string `json:"reality_server_addr,omitempty"`
	// VLESS/VMess REALITY only: the port the REALITY handshake is forwarded to.
	RealityServerPort int `json:"reality_server_port,omitempty"`
	// VLESS/VMess REALITY, server side only: the server's X25519 private key.
	RealityPrivateKey string `json:"reality_private_key,omitempty"`
	// VLESS/VMess REALITY client information: the public key of the private key, mainly for the subscription output.
	RealityPublicKey string `json:"reality_public_key,omitempty"`
	// VLESS/VMess REALITY only: the short ID clients may use.
	RealityShortId string `json:"reality_short_id,omitempty"`
	// VLESS/VMess/Trojan transport: tcp, ws, httpupgrade, grpc or xhttp.
	Transport string `json:"transport,omitempty"`
	// VLESS/VMess/Trojan transport: the Host of WebSocket, HTTPUpgrade or XHTTP.
	Host string `json:"host,omitempty"`
	// VLESS/VMess/Trojan transport: the request path of WebSocket, HTTPUpgrade or XHTTP.
	Path string `json:"path,omitempty"`
	// VLESS/VMess/Trojan gRPC transport only: the gRPC service name.
	ServiceName string `json:"service_name,omitempty"`
	// Shadowsocks/SSR: the Shadowsocks method or the SSR cipher.
	Cipher string `json:"cipher,omitempty"`
	// Key-based protocols: the Shadowsocks 2022 server key, the SSR password or the Snell PSK.
	ServerKey string `json:"server_key,omitempty"`
	// Shadowsocks (AEAD/2022) only: the inbound plugin, such as obfs, v2ray-plugin, shadow-tls or restls.
	Plugin string `json:"plugin,omitempty"`
	// Shadowsocks (AEAD/2022) only: the structured options of the selected inbound plugin.
	PluginOptions any `json:"plugin_opts,omitempty"`
	// VLESS only: the XTLS Vision flow control; xtls-rprx-vision is currently the only valid value.
	Flow string `json:"flow,omitempty"`
	// Protocol-independent capability: UDP over TCP, for every protocol that supports UoT rather than one in particular.
	UoT bool `json:"uot,omitempty"`
	// Protocol-independent capability: the UoT version, currently 1 or 2; 0 means the default version.
	UoTVersion int `json:"uot_version,omitempty"`
	// Listener compatibility: whether to accept the PROXY protocol; node inbounds do not all enable it yet.
	AcceptProxyProtocol bool `json:"accept_proxy_protocol,omitempty"`
	// Hysteria2/TUIC-style QUIC protocols: the port hopping range; node inbounds do not enable port hopping yet.
	HopPorts string `json:"hop_ports,omitempty"`
	// Hysteria2/TUIC-style QUIC protocols: the port hopping interval; node inbounds do not enable port hopping yet.
	HopInterval int `json:"hop_interval,omitempty"`
	// Hysteria2 only: the Salamander obfuscation password, used only with obfs=salamander.
	ObfsPassword string `json:"obfs_password,omitempty"`
	// TLS client compatibility: disables SNI; server inbounds currently ignore it.
	DisableSNI bool `json:"disable_sni,omitempty"`
	// TUIC only: enables QUIC 0-RTT to save round trips on the first handshake.
	ReduceRtt bool `json:"reduce_rtt,omitempty"`
	// TUIC only: the connection heartbeat interval in seconds; 0 uses the node's default.
	Heartbeat int `json:"heartbeat,omitempty"`
	// TUIC/Hysteria compatibility: the UDP relay mode of the former implementation; node inbounds currently ignore it.
	UDPRelayMode string `json:"udp_relay_mode,omitempty"`
	// QUIC protocols: TUIC's congestion control field, and the legacy alias of Naive's.
	CongestionController string `json:"congestion_controller,omitempty"`
	// QUIC protocols: Naive's congestion control field, and the compatibility alias of TUIC's.
	QUICCongestionControl string `json:"quic_congestion_control,omitempty"`
	// Protocol-independent capability: the multiplexing level (off, low, medium or high), for the stream protocols that support mux.
	Multiplex string `json:"multiplex,omitempty"`
	// AnyTLS only: the TLS record padding scheme.
	PaddingScheme string `json:"padding_scheme,omitempty"`
	// Mieru only: the traffic pattern (packet length distribution) settings.
	TrafficPattern string `json:"traffic_pattern,omitempty"`
	// Mieru only: whether clients must send a user hint that identifies the user.
	UserHintIsMandatory bool `json:"user_hint_is_mandatory,omitempty"`
	// Hysteria2 only: the server's upload bandwidth in Mbps.
	UpMbps int `json:"up_mbps,omitempty"`
	// Hysteria2 only: the server's download bandwidth in Mbps.
	DownMbps int `json:"down_mbps,omitempty"`
	// Obfuscating protocols: Hysteria2's Salamander, Snell v5's obfs, SSR's obfs method, or Nowhere's morph (default) / none.
	Obfs string `json:"obfs,omitempty"`
	// SSR only: the SSR protocol method, under the JSON name protocol, which is not the top-level type.
	SSRProtocol string `json:"protocol,omitempty"`
	// SSR only: the SSR protocol_param.
	ProtocolParam string `json:"protocol_param,omitempty"`
	// SSR only: the SSR obfs_param.
	ObfsParam string `json:"obfs_param,omitempty"`
	// Legacy obfuscation compatibility: the obfuscation Host; node inbounds currently ignore it, and Shadowsocks plugins take plugin_opts instead.
	ObfsHost string `json:"obfs_host,omitempty"`
	// Legacy obfuscation compatibility: the obfuscation request path; node inbounds currently ignore it, and Shadowsocks plugins take plugin_opts instead.
	ObfsPath string `json:"obfs_path,omitempty"`
	// VLESS/VMess/Trojan XHTTP transport only: the XHTTP mode, such as auto, packet-up or stream-up.
	XhttpMode string `json:"xhttp_mode,omitempty"`
	// VLESS/VMess/Trojan XHTTP transport only: the XHTTP extra path and parameters.
	XhttpExtra string `json:"xhttp_extra,omitempty"`
	// VLESS Encryption only: the cipher suite, such as none or mlkem768x25519plus.
	Encryption string `json:"encryption,omitempty"`
	// VLESS Encryption only: the key encapsulation mode, such as native, xorpub or random.
	EncryptionMode string `json:"encryption_mode,omitempty"`
	// VLESS Encryption only: the handshake round-trip mode, 0rtt or 1rtt.
	EncryptionRtt string `json:"encryption_rtt,omitempty"`
	// VLESS Encryption, server side only: the 0-RTT ticket.
	EncryptionTicket string `json:"encryption_ticket,omitempty"`
	// VLESS Encryption, server side only: the padding rules of the server direction.
	EncryptionServerPadding string `json:"encryption_server_padding,omitempty"`
	// VLESS Encryption, server side only: the ML-KEM/X25519 private key material.
	EncryptionPrivateKey string `json:"encryption_private_key,omitempty"`
	// VLESS Encryption client information: the padding rules of the client direction, for the subscription output.
	EncryptionClientPadding string `json:"encryption_client_padding,omitempty"`
	// VLESS Encryption client information: the 1-RTT (derived) authentication password, for the subscription output.
	EncryptionPassword string `json:"encryption_password,omitempty"`
	// Subscription clients: whether Encrypted ClientHello is enabled; left out of the configuration distributed to the nodes.
	EchEnable bool `json:"ech_enable,omitempty"`
	// Subscription clients: the outer ECH server name; left out of the configuration distributed to the nodes.
	EchServerName string `json:"ech_server_name,omitempty"`
	// Panel: the traffic billing ratio, 1 by default; not part of the protocol handshake.
	Ratio float64 `json:"ratio,omitempty"`
	// TLS protocols: the certificate source, file, self, http or dns; none configures no certificate.
	CertMode string `json:"cert_mode,omitempty"`
	// TLS protocols: the DNS provider used with cert_mode=dns.
	CertDNSProvider string `json:"cert_dns_provider,omitempty"`
	// TLS protocols: the environment variables (credentials) passed to the DNS provider with cert_mode=dns.
	CertDNSEnv string `json:"cert_dns_env"`
	// TLS protocols: the SHA256 fingerprint (lowercase hex) of the TLS leaf
	// certificate the node reports, kept only with cert_mode=self, for the
	// subscription clients to pin the certificate. The admin API neither reads
	// nor writes it: partial updates keep it by merging the stored fields back.
	CertPinSHA256 string `json:"cert_pin_sha256,omitempty"`
}

// Marshal returns the protocol's JSON form.
func (m *Protocol) Marshal() ([]byte, error) {
	type Alias Protocol
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// Unmarshal fills the protocol from its JSON form.
func (m *Protocol) Unmarshal(data []byte) error {
	type Alias Protocol
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	return json.Unmarshal(data, &aux)
}

func reorderSortWithServer(tx *gorm.DB) error {
	var servers []Server
	if err := tx.Order("sort, id").Find(&servers).Error; err != nil {
		return err
	}
	for i, server := range servers {
		if server.Sort != i+1 {
			if err := tx.Exec("UPDATE servers SET sort = ? WHERE id = ?", i+1, server.Id).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
