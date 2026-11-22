// Package config defines the server's configuration: Config, whose boot half
// is read from the configuration file at start-up and whose runtime half
// administrators edit in the system settings table; the decoding of stored
// settings into those structs; the Redis keys several modules share; and the
// Redis helpers of start-up and installation. The composition root, the
// transports and the modules all read these definitions, so a setting has one
// name and one default everywhere.
package config

import (
	"encoding/json"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/trace"
)

// Config is the server's whole configuration: the boot settings, read from
// the configuration file once at startup, and the runtime settings, which
// the system settings table overrides at startup and whenever an
// administrator changes them. The two halves are embedded, so their fields
// read as the Config's own; in the file they share one level.
type Config struct {
	Boot    `yaml:",inline"`
	Runtime `yaml:",inline"`
}

// Boot is the configuration fixed for the life of the process: the listener,
// the connections and the secrets. Changing it takes a restart.
type Boot struct {
	Host  string `yaml:"Host" default:"0.0.0.0"`
	Port  int    `yaml:"Port" default:"8080"`
	Debug bool   `yaml:"Debug" default:"false"`
	// TrustedProxies lists the reverse proxies (IP addresses or CIDRs) whose
	// X-Forwarded-For and X-Real-IP headers name the real client. Empty means
	// DefaultTrustedProxies; the entry "private" stands for those networks in a
	// longer list, and ["none"] trusts no header at all.
	TrustedProxies []string `yaml:"TrustedProxies"`
	// AllowedOrigins lists the browser origins CORS admits, as scheme://host[:port].
	// Empty keeps the permissive default of reflecting the request's Origin.
	AllowedOrigins []string            `yaml:"AllowedOrigins"`
	HTTP           HTTPConfig          `yaml:"HTTP"`
	GeoIP          GeoIPConfig         `yaml:"GeoIP"`
	AppLocation    string              `yaml:"AppLocation" default:"Asia/Shanghai"`
	Transport      TransportConfig     `yaml:"Transport"`
	TLS            TLS                 `yaml:"TLS"`
	JwtAuth        JwtAuth             `yaml:"JwtAuth"`
	Logger         logger.LogConf      `yaml:"Logger"`
	Trace          trace.Config        `yaml:"Trace"`
	Database       orm.Config          `yaml:"Database"`
	MySQL          *orm.Config         `yaml:"MySQL,omitempty"` // Deprecated: use Database.
	Redis          RedisConfig         `yaml:"Redis"`
	EdgeSubscribe  EdgeSubscribeConfig `yaml:"EdgeSubscribe"`
	Administrator  AdministratorConfig `yaml:"Administrator"`
}

// AdministratorConfig names the first administrator, whom the start seeds
// into a database that holds no account yet. The setup wizard writes the
// installer's email here, so an installation resumed after a failed
// migration seeds the same account.
type AdministratorConfig struct {
	Email string `yaml:"Email" default:"admin@ppanel.dev"`
	// Password seeds the first administrator. Left empty, a random one is
	// generated and printed once at the first start. Once the administrator
	// exists it seeds nothing, only sits in the file in clear, and the start
	// flags it: remove it from the file then.
	Password string `yaml:"Password,omitempty"`
}

// Runtime is the configuration an administrator edits while the server runs.
// The runtime state publishes it as immutable snapshots; a reload replaces a
// section from the system settings table.
type Runtime struct {
	Site       SiteConfig      `yaml:"Site"`
	Node       NodeConfig      `yaml:"Node"`
	Mobile     MobileConfig    `yaml:"Mobile"`
	Email      EmailConfig     `yaml:"Email"`
	Device     DeviceConfig    `yaml:"device"`
	Verify     Verify          `yaml:"Verify"`
	VerifyCode VerifyCode      `yaml:"VerifyCode"`
	Register   RegisterConfig  `yaml:"Register"`
	Subscribe  SubscribeConfig `yaml:"Subscribe"`
	Invite     InviteConfig    `yaml:"Invite"`
	Telegram   Telegram        `yaml:"Telegram"`
	Log        Log             `yaml:"Log"`
	Currency   Currency        `yaml:"Currency"`
}

type RedisConfig struct {
	Host      string `yaml:"Host" default:"localhost:6379"`
	Username  string `yaml:"Username,omitempty"`
	KeyPrefix string `yaml:"KeyPrefix,omitempty"`
	Pass      string `yaml:"Pass" default:""`
	DB        int    `yaml:"DB" default:"0"`
	// QueueDB is the Redis database of the asynq task queue, shared by the
	// producer, the consumer and the scheduler. It defaults to the database
	// the server always used, so an upgrade with no prefix keeps its queued tasks.
	QueueDB int `yaml:"QueueDB" default:"5"`
}

// DefaultTrustedProxies are the networks trusted as reverse proxies when
// TrustedProxies is empty: the loopback interface and the private ranges, so
// nginx on the same host, a Docker bridge or an internal load balancer is
// trusted without configuration. Clients reach a proxy panel over the public
// network, so a peer from these ranges is a proxy, not a client; a server its
// clients reach directly from a private network sets TrustedProxies to
// ["none"].
var DefaultTrustedProxies = []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// HTTPConfig bounds the API listener. The defaults are the values the server
// ran with before they were configurable.
type HTTPConfig struct {
	ReadTimeoutSeconds int `yaml:"ReadTimeoutSeconds" default:"180"`
	// WriteTimeoutSeconds 0 leaves response writes unbounded, which the
	// streaming endpoints (order events over SSE, WebSocket) need.
	WriteTimeoutSeconds int `yaml:"WriteTimeoutSeconds" default:"0"`
	IdleTimeoutSeconds  int `yaml:"IdleTimeoutSeconds" default:"180"`
	MaxRequestBodyMB    int `yaml:"MaxRequestBodyMB" default:"4"`
}

// GeoIPConfig locates the MaxMind city database that enriches the audit
// logs with a location.
type GeoIPConfig struct {
	// Path of the GeoLite2-City.mmdb file.
	Path string `yaml:"Path" default:"./cache/GeoLite2-City.mmdb"`
	// Download fetches the database from DownloadURL (the built-in mirror when
	// empty) when Path is missing.
	Download    bool   `yaml:"Download" default:"true"`
	DownloadURL string `yaml:"DownloadURL" default:""`
	// SHA256 is the hex digest the file must have; empty skips the check.
	SHA256 string `yaml:"SHA256" default:""`
	// Required makes a missing or invalid database fatal at start-up; by
	// default the server starts without geolocation and logs the reason.
	Required bool `yaml:"Required" default:"false"`
}

type TransportConfig struct {
	Driver string `yaml:"Driver" default:"hertz"`
}

type JwtAuth struct {
	AccessSecret string `yaml:"AccessSecret"`
	AccessExpire int64  `yaml:"AccessExpire" default:"604800"`
}

type Verify struct {
	TurnstileSiteKey    string `yaml:"TurnstileSiteKey" default:""`
	TurnstileSecret     string `yaml:"TurnstileSecret" default:""`
	LoginVerify         bool   `yaml:"LoginVerify" default:"false"`
	RegisterVerify      bool   `yaml:"RegisterVerify" default:"false"`
	ResetPasswordVerify bool   `yaml:"ResetPasswordVerify" default:"false"`
}

type SubscribeConfig struct {
	SingleModel           bool   `yaml:"SingleModel" default:"false"`
	SubscribePath         string `yaml:"SubscribePath" default:"/v1/subscribe/config"`
	SubscribeDomain       string `yaml:"SubscribeDomain" default:""`
	PanDomain             bool   `yaml:"PanDomain" default:"false"`
	UserAgentLimit        bool   `yaml:"UserAgentLimit" default:"false"`
	UserAgentList         string `yaml:"UserAgentList" default:""`
	ShowTutorial          bool   `yaml:"ShowTutorial" default:"true"`
	ProfileUpdateInterval int64  `yaml:"ProfileUpdateInterval" default:"0"`
	ProfileWebPageURL     string `yaml:"ProfileWebPageURL" default:""`
}

// EdgeSubscribeConfig configures the private manifest API consumed by the
// Cloudflare edge-subscribe Worker. It intentionally has no relationship to
// the user-facing subscription template configuration above.
type EdgeSubscribeConfig struct {
	Enabled             bool                     `yaml:"Enabled" default:"false"`
	MaxClockSkewSeconds int64                    `yaml:"MaxClockSkewSeconds" default:"300"`
	Keys                []EdgeSubscribeAccessKey `yaml:"Keys"`
}

// EdgeSubscribeAccessKey is an HMAC credential owned by a Worker deployment.
// Secret must be stored only in the PPanel runtime config and the Worker secret
// store; it must never be returned from an API.
type EdgeSubscribeAccessKey struct {
	ID     string `yaml:"ID"`
	Secret string `yaml:"Secret"`
}

type RegisterConfig struct {
	StopRegister            bool   `yaml:"StopRegister" default:"false"`
	EnableTrial             bool   `yaml:"EnableTrial" default:"false"`
	TrialSubscribe          int64  `yaml:"TrialSubscribe" default:"0"`
	TrialTime               int64  `yaml:"TrialTime" default:"0"`
	TrialTimeUnit           string `yaml:"TrialTimeUnit" default:""`
	IpRegisterLimit         int64  `yaml:"IpRegisterLimit" default:"0"`
	IpRegisterLimitDuration int64  `yaml:"IpRegisterLimitDuration" default:"0"`
	EnableIpRegisterLimit   bool   `yaml:"EnableIpRegisterLimit" default:"false"`
}

type EmailConfig struct {
	Enable                     bool   `yaml:"Enable" default:"true"`
	Platform                   string `yaml:"platform"`
	PlatformConfig             string `yaml:"platform_config"`
	EnableVerify               bool   `yaml:"enable_verify"`
	EnableNotify               bool   `yaml:"enable_notify"`
	EnableDomainSuffix         bool   `yaml:"enable_domain_suffix"`
	DomainSuffixList           string `yaml:"domain_suffix_list"`
	VerifyEmailTemplate        string `yaml:"verify_email_template"`
	ExpirationEmailTemplate    string `yaml:"expiration_email_template"`
	MaintenanceEmailTemplate   string `yaml:"maintenance_email_template"`
	TrafficExceedEmailTemplate string `yaml:"traffic_exceed_email_template"`
	// Subjects pair with the templates above. They are copied from the email
	// auth method config by field name (internal/infra/mapping), so the
	// fields must keep the names they have there; empty means the queued
	// fallback subject is used.
	VerifyEmailSubject        string `yaml:"verify_email_subject"`
	ExpirationEmailSubject    string `yaml:"expiration_email_subject"`
	MaintenanceEmailSubject   string `yaml:"maintenance_email_subject"`
	TrafficExceedEmailSubject string `yaml:"traffic_exceed_email_subject"`
}

type MobileConfig struct {
	Enable          bool     `yaml:"Enable" default:"true"`
	Platform        string   `yaml:"platform"`
	PlatformConfig  string   `yaml:"platform_config"`
	EnableVerify    bool     `yaml:"enable_verify"`
	EnableWhitelist bool     `yaml:"enable_whitelist"`
	Whitelist       []string `yaml:"whitelist"`
}

type DeviceConfig struct {
	Enable         bool   `yaml:"enable" default:"true"`
	ShowAds        bool   `yaml:"show_ads"`
	EnableSecurity bool   `yaml:"enable_security"`
	OnlyRealDevice bool   `yaml:"only_real_device"`
	SecuritySecret string `yaml:"security_secret"`
}

type SiteConfig struct {
	Host       string `yaml:"Host" default:""`
	SiteName   string `yaml:"SiteName" default:""`
	SiteDesc   string `yaml:"SiteDesc" default:""`
	SiteLogo   string `yaml:"SiteLogo" default:""`
	Keywords   string `yaml:"Keywords" default:""`
	CustomHTML string `yaml:"CustomHTML" default:""`
	CustomData string `yaml:"CustomData" default:""`
}

type NodeConfig struct {
	NodeSecret             string         `yaml:"NodeSecret" default:""`
	NodePullInterval       int64          `yaml:"NodePullInterval" default:"60"`
	NodePushInterval       int64          `yaml:"NodePushInterval" default:"60"`
	TrafficReportThreshold int64          `yaml:"TrafficReportThreshold" default:"0"`
	IPStrategy             string         `yaml:"IPStrategy" default:""`
	DNS                    []NodeDNS      `yaml:"DNS"`
	Block                  []string       `yaml:"Block" `
	Outbound               []NodeOutbound `yaml:"Outbound"`
}

func (n *NodeConfig) Marshal() ([]byte, error) {
	type Alias NodeConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(n),
	})
}

func (n *NodeConfig) Unmarshal(data []byte) error {
	type Alias NodeConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(n),
	}
	return json.Unmarshal(data, &aux)
}

type NodeDNS struct {
	Proto      string   `json:"proto"`
	Address    string   `json:"address"`
	ServerName string   `json:"server_name,omitempty"`
	Domains    []string `json:"domains"`
}

func (n *NodeDNS) Marshal() ([]byte, error) {
	type Alias NodeDNS
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(n),
	})
}

func (n *NodeDNS) Unmarshal(data []byte) error {
	type Alias NodeDNS
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(n),
	}
	return json.Unmarshal(data, &aux)
}

type NodeOutbound struct {
	Name                 string   `json:"name"`
	Protocol             string   `json:"protocol"`
	Address              string   `json:"address"`
	Port                 int64    `json:"port"`
	User                 string   `json:"user,omitempty"`
	Password             string   `json:"password"`
	UUID                 string   `json:"uuid,omitempty"`
	Cipher               string   `json:"cipher,omitempty"`
	Plugin               string   `json:"plugin,omitempty"`
	PluginOptions        any      `json:"plugin_opts,omitempty"`
	Security             string   `json:"security,omitempty"`
	SNI                  string   `json:"sni,omitempty"`
	ALPN                 []string `json:"alpn,omitempty"`
	AllowInsecure        bool     `json:"allow_insecure,omitempty"`
	Fingerprint          string   `json:"fingerprint,omitempty"`
	Transport            string   `json:"transport,omitempty"`
	Host                 string   `json:"host,omitempty"`
	Path                 string   `json:"path,omitempty"`
	ServiceName          string   `json:"service_name,omitempty"`
	XHTTPMode            string   `json:"xhttp_mode,omitempty"`
	XHTTPExtra           string   `json:"xhttp_extra,omitempty"`
	Flow                 string   `json:"flow,omitempty"`
	Encryption           string   `json:"encryption,omitempty"`
	EncryptionMode       string   `json:"encryption_mode,omitempty"`
	EncryptionRTT        string   `json:"encryption_rtt,omitempty"`
	EncryptionTicket     string   `json:"encryption_ticket,omitempty"`
	EncryptionPadding    string   `json:"encryption_client_padding,omitempty"`
	EncryptionPassword   string   `json:"encryption_password,omitempty"`
	Multiplex            string   `json:"multiplex,omitempty"`
	UoT                  bool     `json:"uot,omitempty"`
	UoTVersion           int      `json:"uot_version,omitempty"`
	CongestionController string   `json:"congestion_controller,omitempty"`
	UDPStream            bool     `json:"udp_stream,omitempty"`
	ReduceRtt            bool     `json:"reduce_rtt,omitempty"`
	Heartbeat            int      `json:"heartbeat,omitempty"`
	RealityPublicKey     string   `json:"reality_public_key,omitempty"`
	RealityShortId       string   `json:"reality_short_id,omitempty"`
	SpiderX              string   `json:"spider_x,omitempty"`
	Settings             string   `json:"settings,omitempty"`
	StreamSettings       string   `json:"stream_settings,omitempty"`
	Rules                []string `json:"rules"`
}

func (n *NodeOutbound) Marshal() ([]byte, error) {
	type Alias NodeOutbound
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(n),
	})
}

type File struct {
	Host           string              `yaml:"Host" default:"0.0.0.0"`
	Port           int                 `yaml:"Port" default:"8080"`
	Transport      TransportConfig     `yaml:"Transport"`
	TLS            TLS                 `yaml:"TLS"`
	Debug          bool                `yaml:"Debug" default:"false"`
	TrustedProxies []string            `yaml:"TrustedProxies"`
	AllowedOrigins []string            `yaml:"AllowedOrigins"`
	HTTP           HTTPConfig          `yaml:"HTTP"`
	GeoIP          GeoIPConfig         `yaml:"GeoIP"`
	JwtAuth        JwtAuth             `yaml:"JwtAuth"`
	Logger         logger.LogConf      `yaml:"Logger"`
	Trace          trace.Config        `yaml:"Trace"`
	Database       orm.Config          `yaml:"Database"`
	MySQL          *orm.Config         `yaml:"MySQL,omitempty"` // Deprecated: use Database.
	Redis          RedisConfig         `yaml:"Redis"`
	EdgeSubscribe  EdgeSubscribeConfig `yaml:"EdgeSubscribe"`
	// AppLocation survives the installation's rewrite of the file: the new
	// database's session zone is set from it.
	AppLocation   string              `yaml:"AppLocation" default:"Asia/Shanghai"`
	Administrator AdministratorConfig `yaml:"Administrator"`
}

func (c Config) DatabaseConfig() orm.Config {
	if hasDatabaseConfig(c.Database) {
		return c.Database
	}
	if c.MySQL != nil {
		return *c.MySQL
	}
	return c.Database
}

func (c *Config) SetDatabaseConfig(cfg orm.Config) {
	c.Database = cfg
	c.MySQL = nil
}

func (f File) DatabaseConfig() orm.Config {
	if hasDatabaseConfig(f.Database) {
		return f.Database
	}
	if f.MySQL != nil {
		return *f.MySQL
	}
	return f.Database
}

func (f *File) SetDatabaseConfig(cfg orm.Config) {
	f.Database = cfg
	f.MySQL = nil
}

func hasDatabaseConfig(cfg orm.Config) bool {
	return cfg.Addr != "" || cfg.Dbname != "" || cfg.Username != "" || cfg.Password != ""
}

type InviteConfig struct {
	ForcedInvite       bool   `yaml:"ForcedInvite" default:"false"`
	ReferralPercentage int64  `yaml:"ReferralPercentage" default:"0"`
	OnlyFirstPurchase  bool   `yaml:"OnlyFirstPurchase" default:"false"`
	WithdrawalMethod   string `yaml:"WithdrawalMethod" default:""`
}

type Telegram struct {
	Enable        bool   `yaml:"Enable" default:"false"`
	BotID         int64  `yaml:"BotID" default:""`
	BotName       string `yaml:"BotName" default:""`
	BotToken      string `yaml:"BotToken" default:""`
	EnableNotify  bool   `yaml:"EnableNotify" default:"false"`
	WebHookDomain string `yaml:"WebHookDomain" default:""`
	// GroupChatID is the validated administrators' group; zero when the
	// group is unconfigured or failed validation, which disables every
	// group-side feature (admin commands, topics, notifications).
	GroupChatID int64 `yaml:"GroupChatID" default:"0"`
}

type TLS struct {
	Enable   bool   `yaml:"Enable" default:"false"`
	CertFile string `yaml:"CertFile" default:""`
	KeyFile  string `yaml:"KeyFile" default:""`
}

type VerifyCode struct {
	ExpireTime int64 `yaml:"ExpireTime" default:"300"`
	Limit      int64 `yaml:"Limit" default:"15"`
	Interval   int64 `yaml:"Interval" default:"60"`
}

type Log struct {
	AutoClear bool  `yaml:"AutoClear" default:"true"`
	ClearDays int64 `yaml:"ClearDays" default:"7"`
}

type NodeDBConfig struct {
	NodeSecret             string
	NodePullInterval       int64
	NodePushInterval       int64
	TrafficReportThreshold int64
	IPStrategy             string
	DNS                    string
	Block                  string
	Outbound               string
}

// Currency is the site currency: the ISO code prices are in and the symbol
// shown next to them. The defaults are the values the settings seed stores
// (CurrencyUnit USD, CurrencySymbol $), so a runtime configuration not yet
// loaded from the table agrees with a fresh installation; they used to
// disagree with each other (a CNY unit with a "USD" symbol).
type Currency struct {
	Unit      string `yaml:"Unit" default:"USD"`
	Symbol    string `yaml:"Symbol" default:"$"`
	AccessKey string `yaml:"AccessKey" default:""`
}
