package adapter

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
)

type Proxy struct {
	Sort   int
	Name   string
	Server string
	Port   uint16
	Type   string
	Tags   []string

	// Security Options
	Security          string
	SNI               string // Server Name Indication for TLS
	AllowInsecure     bool   // Allow insecure connections (skip certificate verification)
	Fingerprint       string // Client fingerprint for TLS connections
	RealityServerAddr string // Reality server address
	RealityServerPort int    // Reality server port
	RealityPrivateKey string // Reality private key for authentication
	RealityPublicKey  string // Reality public key for authentication
	RealityShortId    string // Reality short ID for authentication
	// Transport Options
	Transport   string // Transport protocol (e.g., ws, http, grpc)
	Host        string // For WebSocket/HTTP/HTTPS
	Path        string // For HTTP/HTTPS
	ServiceName string // For gRPC
	// Shadowsocks Options
	Method     string
	ServerKey  string // For Shadowsocks 2022
	UoT        bool   // UDP over TCP
	UoTVersion int    // UoT version (1 or 2)

	// Vmess/Vless/Trojan Options
	Flow string // Flow for Vmess/Vless/Trojan
	// Hysteria2 Options
	HopPorts     string // Comma-separated list of hop ports
	HopInterval  int    // Interval for hop ports in seconds
	ObfsPassword string // Obfuscation password for Hysteria2
	UpMbps       int    // Upload speed in Mbps
	DownMbps     int    // Download speed in Mbps

	// Tuic Options
	DisableSNI           bool   // Disable SNI
	ReduceRtt            bool   // Reduce RTT
	UDPRelayMode         string // UDP relay mode (e.g., "full", "partial")
	CongestionController string // Congestion controller (e.g., "cubic", "bbr")

	// AnyTLS
	PaddingScheme string

	// Mieru
	Multiplex string

	// Obfs
	//Obfs     string // obfs, 'none', 'http', 'tls'
	//ObfsHost string // obfs host
	//ObfsPath string // obfs path

	// Vless
	XhttpMode  string // xhttp mode
	XhttpExtra string // xhttp path

	// encryption
	Encryption              string // encryption，'none', 'mlkem768x25519plus'
	EncryptionMode          string // encryption mode，'native', 'xorpub', 'random'
	EncryptionRtt           string // encryption rtt，'0rtt', '1rtt'
	EncryptionTicket        string // encryption ticket
	EncryptionServerPadding string // encryption server padding
	EncryptionPrivateKey    string // encryption private key
	EncryptionClientPadding string // encryption client padding
	EncryptionPassword      string // encryption password

	// ECH
	EchEnable     bool   // ECH enable
	EchServerName string // ECH SNI

	Ratio           float64 // Traffic ratio, default is 1
	CertMode        string  // Certificate mode, `none`｜`http`｜`dns`｜`self`
	CertDNSProvider string  // DNS provider for certificate
	CertDNSEnv      string  // Environment for DNS provider
}

type User struct {
	Password     string
	ExpiredAt    time.Time
	Download     int64
	Upload       int64
	Traffic      int64
	SubscribeURL string
}

type Client struct {
	SiteName       string            // Name of the site
	SubscribeName  string            // Name of the subscription
	ClientTemplate string            // Template for the entire client configuration
	OutputFormat   string            // json, yaml, etc.
	Proxies        []Proxy           // List of proxy configurations
	UserInfo       User              // User information
	Params         map[string]string // Additional parameters
}

func (c *Client) Build() ([]byte, error) {
	var buf bytes.Buffer
	tmpl, err := template.New("client").Funcs(sprig.TxtFuncMap()).Parse(c.ClientTemplate)
	if err != nil {
		return nil, err
	}

	proxies := make([]map[string]interface{}, len(c.Proxies))
	for i, p := range c.Proxies {
		proxies[i] = StructToMap(p)
	}

	err = tmpl.Execute(&buf, map[string]interface{}{
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

func StructToMap(obj interface{}) map[string]interface{} {
	m := make(map[string]interface{})
	v := reflect.ValueOf(obj)
	t := reflect.TypeOf(obj)

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		m[field.Name] = v.Field(i).Interface()
	}
	return m
}
