package render

import (
	"bytes"
	"encoding/base64"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
)

type Proxy struct {
	Sort    int
	Name    string
	Server  string
	Port    uint16
	Type    string
	Tags    []string
	Version int
	Mode    string
	Network string

	// Security Options
	Security         string
	SNI              string // Server Name Indication for TLS
	ALPN             []string
	AllowInsecure    bool   // Allow insecure connections (skip certificate verification)
	Fingerprint      string // Client fingerprint for TLS connections
	RealityPublicKey string // Reality public key for authentication
	RealityShortId   string // Reality short ID for authentication
	// Transport Options
	Transport   string // Transport protocol (e.g., ws, http, grpc)
	Host        string // For WebSocket/HTTP/HTTPS
	Path        string // For HTTP/HTTPS
	ServiceName string // For gRPC
	// Shadowsocks Options
	Method              string
	ServerKey           string // For Shadowsocks 2022
	Plugin              string
	PluginOptions       any
	UoT                 bool // UDP over TCP
	UoTVersion          int  // UoT version (1 or 2)
	AcceptProxyProtocol bool

	// Vmess/Vless/Trojan Options
	Flow string // Flow for Vmess/Vless/Trojan
	// Hysteria2 Options
	HopPorts     string // Comma-separated list of hop ports
	HopInterval  int    // Interval for hop ports in seconds
	ObfsPassword string // Obfuscation password for Hysteria2
	UpMbps       int    // Upload speed in Mbps
	DownMbps     int    // Download speed in Mbps

	// Tuic Options
	DisableSNI            bool // Disable SNI
	ReduceRtt             bool // Reduce RTT
	Heartbeat             int
	UDPRelayMode          string // UDP relay mode (e.g., "full", "partial")
	CongestionController  string // Congestion controller (e.g., "cubic", "bbr")
	QUICCongestionControl string

	// AnyTLS
	PaddingScheme string

	// Protocol-independent multiplexing and Mieru options.
	Multiplex           string
	TrafficPattern      string
	UserHintIsMandatory bool

	// ShadowsocksR and compatible obfuscation options.
	Obfs          string
	SSRProtocol   string
	ProtocolParam string
	ObfsParam     string
	ObfsHost      string
	ObfsPath      string

	// Vless
	XhttpMode  string // xhttp mode
	XhttpExtra string // xhttp path

	// encryption
	Encryption              string // encryption, 'none', 'mlkem768x25519plus'
	EncryptionMode          string // encryption mode, 'native', 'xorpub', 'random'
	EncryptionRtt           string // encryption rtt, '0rtt', '1rtt'
	EncryptionClientPadding string // encryption client padding
	EncryptionPassword      string // encryption password

	// ECH
	EchEnable     bool   // ECH enable
	EchServerName string // ECH SNI

	Ratio    float64 // Traffic ratio, default is 1
	CertMode string  // Certificate mode, `none`|`http`|`dns`|`self`
	// The DNS provider and its credentials (CertDNSProvider, CertDNSEnv) and
	// the REALITY handshake target (RealityServerAddr, RealityServerPort)
	// stay on the server: a client only needs the pin below, the SNI and the
	// REALITY public key and short id.
	CertPinSHA256 string // SHA256 fingerprint of the self-signed certificate (lowercase hex)
}

type User struct {
	ID           int64
	Password     string
	ExpiredAt    time.Time
	Download     int64
	Upload       int64
	Traffic      int64
	SubscribeURL string
}

// NowhereKey is the protocol-specific NW3 shared key. Password retains the
// original subscription UUID for all other protocols and mixed subscriptions.
// Templates access this method as .UserInfo.NowhereKey; invalid input fails
// template execution rather than producing an unusable credential.
func (u User) NowhereKey() (string, error) { return node.NowhereSharedKey(u.Password) }

type Client struct {
	SiteName       string            // Name of the site
	SubscribeName  string            // Name of the subscription
	ClientTemplate string            // Template for the entire client configuration
	OutputFormat   string            // json, yaml, etc.
	Proxies        []Proxy           // List of proxy configurations
	UserInfo       User              // User information
	Params         map[string]string // Additional parameters
}

// templateFuncs is sprig's text function map without the functions that
// reach outside the template: env and expandenv read the server process
// environment (env-based deployments keep the database and Redis
// credentials there) and getHostByName performs DNS lookups. Templates are
// admin-authored and their output goes to every subscriber, so none of these
// may be reachable. The other non-hermetic helpers stay: the seeded
// templates call now and date.
var templateFuncs = func() template.FuncMap {
	funcs := sprig.TxtFuncMap()
	for _, name := range []string{"env", "expandenv", "getHostByName"} {
		delete(funcs, name)
	}
	return funcs
}()

func (c *Client) Build() ([]byte, error) {
	var buf bytes.Buffer
	tmpl, err := parsedTemplates.get(c.ClientTemplate)
	if err != nil {
		return nil, err
	}

	proxies := make([]map[string]any, len(c.Proxies))
	for i, p := range c.Proxies {
		proxies[i] = p.templateData()
	}

	err = tmpl.Execute(&buf, map[string]any{
		"SiteName":      c.SiteName,
		"SubscribeName": c.SubscribeName,
		"OutputFormat":  c.OutputFormat,
		"Proxies":       proxies,
		"UserInfo":      c.UserInfo,
		"Params":        c.Params,
	})
	if err != nil {
		return nil, err
	}

	result := buf.String()
	if c.OutputFormat == "base64" {
		encoded := base64.StdEncoding.EncodeToString([]byte(result))
		return []byte(encoded), nil
	}

	return buf.Bytes(), nil
}
