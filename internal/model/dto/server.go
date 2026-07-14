package dto

type AnyTLS struct {
	Port           int            `json:"port" validate:"required"`
	SecurityConfig SecurityConfig `json:"security_config"`
}

type CreateServerRequest struct {
	Name      string     `json:"name"`
	Country   string     `json:"country,omitempty"`
	City      string     `json:"city,omitempty"`
	Address   string     `json:"address"`
	Sort      int        `json:"sort,omitempty"`
	Protocols []Protocol `json:"protocols"`
}

type DeleteServerRequest struct {
	Id int64 `json:"id"`
}

type FilterServerListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Search string `form:"search,omitempty"`
}

type FilterServerListResponse struct {
	Total int64    `json:"total"`
	List  []Server `json:"list"`
}

type GetServerConfigRequest struct {
	ServerCommon
}

type GetServerConfigResponse struct {
	Basic    ServerBasic `json:"basic"`
	Protocol string      `json:"protocol"`
	Config   interface{} `json:"config"`
}

type GetServerProtocolsRequest struct {
	Id int64 `form:"id"`
}

type GetServerProtocolsResponse struct {
	Protocols []Protocol `json:"protocols"`
}

type GetServerUserListRequest struct {
	ServerCommon
}

type GetServerUserListResponse struct {
	Users []ServerUser `json:"users"`
}

type Hysteria2 struct {
	Port           int            `json:"port" validate:"required"`
	HopPorts       string         `json:"hop_ports" validate:"required"`
	HopInterval    int            `json:"hop_interval" validate:"required"`
	ObfsPassword   string         `json:"obfs_password" validate:"required"`
	SecurityConfig SecurityConfig `json:"security_config"`
}

type MigrateServerNodeResponse struct {
	Succee  uint64 `json:"succee"`
	Fail    uint64 `json:"fail"`
	Message string `json:"message,omitempty"`
}

type ServerNodeConfigValues struct {
	IPStrategy string         `json:"ip_strategy"`
	DNS        []NodeDNS      `json:"dns"`
	Block      []string       `json:"block"`
	Outbound   []NodeOutbound `json:"outbound"`
}

type ServerNodeConfigOverride struct {
	InheritIPStrategy bool   `json:"inherit_ip_strategy"`
	IPStrategy        string `json:"ip_strategy"`

	InheritDNS bool      `json:"inherit_dns"`
	DNS        []NodeDNS `json:"dns"`

	InheritBlock bool     `json:"inherit_block"`
	Block        []string `json:"block"`

	InheritOutbound bool           `json:"inherit_outbound"`
	Outbound        []NodeOutbound `json:"outbound"`
}

type OnlineUser struct {
	SID int64  `json:"uid"`
	IP  string `json:"ip"`
}

type OnlineUsersRequest struct {
	ServerCommon
	Users []OnlineUser `json:"users"`
}

type Protocol struct {
	Type                    string  `json:"type"`
	Port                    uint16  `json:"port"`
	Enable                  bool    `json:"enable"`
	Security                string  `json:"security,omitempty"`
	SNI                     string  `json:"sni,omitempty"`
	AllowInsecure           bool    `json:"allow_insecure,omitempty"`
	Fingerprint             string  `json:"fingerprint,omitempty"`
	RealityServerAddr       string  `json:"reality_server_addr,omitempty"`
	RealityServerPort       int     `json:"reality_server_port,omitempty"`
	RealityPrivateKey       string  `json:"reality_private_key,omitempty"`
	RealityPublicKey        string  `json:"reality_public_key,omitempty"`
	RealityShortId          string  `json:"reality_short_id,omitempty"`
	Transport               string  `json:"transport,omitempty"`
	Host                    string  `json:"host,omitempty"`
	Path                    string  `json:"path,omitempty"`
	ServiceName             string  `json:"service_name,omitempty"`
	Cipher                  string  `json:"cipher,omitempty"`
	ServerKey               string  `json:"server_key,omitempty"`
	Flow                    string  `json:"flow,omitempty"`
	UoT                     bool    `json:"uot,omitempty"`                   // UDP over TCP
	UoTVersion              int     `json:"uot_version,omitempty"`           // UoT version (1 or 2)
	AcceptProxyProtocol     bool    `json:"accept_proxy_protocol,omitempty"` // accept proxy protocol
	HopPorts                string  `json:"hop_ports,omitempty"`
	HopInterval             int     `json:"hop_interval,omitempty"`
	ObfsPassword            string  `json:"obfs_password,omitempty"`
	DisableSNI              bool    `json:"disable_sni,omitempty"`
	ReduceRtt               bool    `json:"reduce_rtt,omitempty"`
	UDPRelayMode            string  `json:"udp_relay_mode,omitempty"`
	CongestionController    string  `json:"congestion_controller,omitempty"`
	Multiplex               string  `json:"multiplex,omitempty"`                 // mux, eg: off/low/medium/high
	PaddingScheme           string  `json:"padding_scheme,omitempty"`            // padding scheme
	UpMbps                  int     `json:"up_mbps,omitempty"`                   // upload speed limit
	DownMbps                int     `json:"down_mbps,omitempty"`                 // download speed limit
	Obfs                    string  `json:"obfs,omitempty"`                      // obfs, 'none', 'http', 'tls'
	ObfsHost                string  `json:"obfs_host,omitempty"`                 // obfs host
	ObfsPath                string  `json:"obfs_path,omitempty"`                 // obfs path
	XhttpMode               string  `json:"xhttp_mode,omitempty"`                // xhttp mode
	XhttpExtra              string  `json:"xhttp_extra,omitempty"`               // xhttp extra path
	Encryption              string  `json:"encryption,omitempty"`                // encryption，'none', 'mlkem768x25519plus'
	EncryptionMode          string  `json:"encryption_mode,omitempty"`           // encryption mode，'native', 'xorpub', 'random'
	EncryptionRtt           string  `json:"encryption_rtt,omitempty"`            // encryption rtt，'0rtt', '1rtt'
	EncryptionTicket        string  `json:"encryption_ticket,omitempty"`         // encryption ticket
	EncryptionServerPadding string  `json:"encryption_server_padding,omitempty"` // encryption server padding
	EncryptionPrivateKey    string  `json:"encryption_private_key,omitempty"`    // encryption private key
	EncryptionClientPadding string  `json:"encryption_client_padding,omitempty"` // encryption client padding
	EncryptionPassword      string  `json:"encryption_password,omitempty"`       // encryption password
	EchEnable               bool    `json:"ech_enable,omitempty"`                // ECH enable
	EchServerName           string  `json:"ech_server_name,omitempty"`           // ECH server name
	Ratio                   float64 `json:"ratio,omitempty"`                     // Traffic ratio, default is 1
	CertMode                string  `json:"cert_mode,omitempty"`                 // Certificate mode, `none`｜`http`｜`dns`｜`self`
	CertDNSProvider         string  `json:"cert_dns_provider,omitempty"`         // DNS provider for certificate
	CertDNSEnv              string  `json:"cert_dns_env,omitempty"`              // Environment for DNS provider
}

type QueryServerConfigRequest struct {
	ServerID  int64    `path:"server_id"`
	SecretKey string   `form:"secret_key"`
	Protocols []string `form:"protocols,omitempty"`
}

type QueryServerConfigResponse struct {
	TrafficReportThreshold int64          `json:"traffic_report_threshold"`
	PushInterval           int64          `json:"push_interval"`
	PullInterval           int64          `json:"pull_interval"`
	IPStrategy             string         `json:"ip_strategy"`
	DNS                    []NodeDNS      `json:"dns"`
	Block                  []string       `json:"block"`
	Outbound               []NodeOutbound `json:"outbound"`
	Protocols              []Protocol     `json:"protocols"`
	Total                  int64          `json:"total"`
}

type GetServerNodeConfigRequest struct {
	ServerID int64 `form:"server_id" validate:"required"`
}

type GetServerNodeConfigResponse struct {
	Global    ServerNodeConfigValues   `json:"global"`
	Override  ServerNodeConfigOverride `json:"override"`
	Effective ServerNodeConfigValues   `json:"effective"`
}

type UpdateServerNodeConfigRequest struct {
	ServerID int64 `json:"server_id" validate:"required"`
	ServerNodeConfigOverride
}

type SecurityConfig struct {
	SNI               string `json:"sni"`
	AllowInsecure     *bool  `json:"allow_insecure"`
	Fingerprint       string `json:"fingerprint"`
	RealityServerAddr string `json:"reality_server_addr"`
	RealityServerPort int    `json:"reality_server_port"`
	RealityPrivateKey string `json:"reality_private_key"`
	RealityPublicKey  string `json:"reality_public_key"`
	RealityShortId    string `json:"reality_short_id"`
}

type Server struct {
	Id             int64        `json:"id"`
	Name           string       `json:"name"`
	Country        string       `json:"country"`
	City           string       `json:"city"`
	Address        string       `json:"address"`
	Sort           int          `json:"sort"`
	Protocols      []Protocol   `json:"protocols"`
	LastReportedAt int64        `json:"last_reported_at"`
	Status         ServerStatus `json:"status"`
	CreatedAt      int64        `json:"created_at"`
	UpdatedAt      int64        `json:"updated_at"`
}

type ServerBasic struct {
	PushInterval int64 `json:"push_interval"`
	PullInterval int64 `json:"pull_interval"`
}

type ServerCommon struct {
	Protocol  string `form:"protocol"`
	ServerId  int64  `form:"server_id"`
	SecretKey string `form:"secret_key"`
}

type ServerGroup struct {
	Id          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type ServerOnlineIP struct {
	IP       string `json:"ip"`
	Protocol string `json:"protocol"`
}

type ServerOnlineUser struct {
	IP          []ServerOnlineIP `json:"ip"`
	UserId      int64            `json:"user_id"`
	Subscribe   string           `json:"subscribe"`
	SubscribeId int64            `json:"subscribe_id"`
	Traffic     int64            `json:"traffic"`
	ExpiredAt   int64            `json:"expired_at"`
}

type ServerPushStatusRequest struct {
	ServerCommon
	Cpu       float64 `json:"cpu"`
	Mem       float64 `json:"mem"`
	Disk      float64 `json:"disk"`
	UpdatedAt int64   `json:"updated_at"`
}

type ServerRuleGroup struct {
	Id        int64    `json:"id"`
	Icon      string   `json:"icon"`
	Name      string   `json:"name" validate:"required"`
	Type      string   `json:"type"`
	Tags      []string `json:"tags"`
	Rules     string   `json:"rules"`
	Enable    bool     `json:"enable"`
	Default   bool     `json:"default"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type ServerStatus struct {
	Cpu      float64            `json:"cpu"`
	Mem      float64            `json:"mem"`
	Disk     float64            `json:"disk"`
	Protocol string             `json:"protocol"`
	Online   []ServerOnlineUser `json:"online"`
	Status   string             `json:"status"`
}

type ServerUser struct {
	Id          int64  `json:"id"`
	UUID        string `json:"uuid"`
	SpeedLimit  int64  `json:"speed_limit"`
	DeviceLimit int64  `json:"device_limit"`
}

type Shadowsocks struct {
	Method    string `json:"method" validate:"required"`
	Port      int    `json:"port" validate:"required"`
	ServerKey string `json:"server_key"`
}

type ShadowsocksProtocol struct {
	Port   int    `json:"port"`
	Method string `json:"method"`
}

type TransportConfig struct {
	Path        string `json:"path"`
	Host        string `json:"host"`
	ServiceName string `json:"service_name"`
}

type Trojan struct {
	Port            int             `json:"port" validate:"required"`
	Transport       string          `json:"transport" validate:"required"`
	TransportConfig TransportConfig `json:"transport_config"`
	Security        string          `json:"security" validate:"required"`
	SecurityConfig  SecurityConfig  `json:"security_config"`
}

type TrojanProtocol struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	EnableTLS *bool  `json:"enable_tls"`
	TLSConfig string `json:"tls_config"`
	Network   string `json:"network"`
	Transport string `json:"transport"`
}

type Tuic struct {
	Port                 int            `json:"port" validate:"required"`
	DisableSNI           bool           `json:"disable_sni"`
	ReduceRtt            bool           `json:"reduce_rtt"`
	UDPRelayMode         string         `json:"udp_relay_mode"`
	CongestionController string         `json:"congestion_controller"`
	SecurityConfig       SecurityConfig `json:"security_config"`
}

type UpdateServerRequest struct {
	Id        int64      `json:"id"`
	Name      string     `json:"name"`
	Country   string     `json:"country,omitempty"`
	City      string     `json:"city,omitempty"`
	Address   string     `json:"address"`
	Sort      int        `json:"sort,omitempty"`
	Protocols []Protocol `json:"protocols"`
}

type Vless struct {
	Port            int             `json:"port" validate:"required"`
	Flow            string          `json:"flow" validate:"required"`
	Transport       string          `json:"transport" validate:"required"`
	TransportConfig TransportConfig `json:"transport_config"`
	Security        string          `json:"security" validate:"required"`
	SecurityConfig  SecurityConfig  `json:"security_config"`
}

type VlessProtocol struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Network        string `json:"network"`
	Transport      string `json:"transport"`
	Security       string `json:"security"`
	SecurityConfig string `json:"security_config"`
	XTLS           string `json:"xtls"`
}

type Vmess struct {
	Port            int             `json:"port" validate:"required"`
	Transport       string          `json:"transport" validate:"required"`
	TransportConfig TransportConfig `json:"transport_config"`
	Security        string          `json:"security" validate:"required"`
	SecurityConfig  SecurityConfig  `json:"security_config"`
}

type VmessProtocol struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	EnableTLS *bool  `json:"enable_tls"`
	TLSConfig string `json:"tls_config"`
	Network   string `json:"network"`
	Transport string `json:"transport"`
}
