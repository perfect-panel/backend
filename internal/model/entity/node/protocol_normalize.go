package node

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func NormalizeProtocolForStorage(protocol Protocol) (Protocol, error) {
	protocol.Type = normalizeProtocolType(protocol.Type)
	if !supportedRuntimeProtocol(protocol.Type) {
		return Protocol{}, fmt.Errorf("unsupported protocol type: %s", protocol.Type)
	}
	normalizeProtocolNoopFields(&protocol)
	normalizeProtocolPluginName(&protocol)
	if protocol.Enable {
		if err := validateRuntimeProtocol(&protocol); err != nil {
			return Protocol{}, err
		}
	}
	return protocol, nil
}

func normalizeProtocolPluginName(protocol *Protocol) {
	if protocol.Type != "shadowsocks" {
		protocol.Plugin = ""
		protocol.PluginOptions = nil
		return
	}
	protocol.Plugin = normalizeShadowsocksPluginName(protocol.Plugin)
}

func SanitizeProtocolsForNodeDistribution(protocols []Protocol) []Protocol {
	result := make([]Protocol, 0, len(protocols))
	for _, protocol := range protocols {
		protocol, err := NormalizeProtocolForStorage(protocol)
		if err != nil || !protocol.Enable {
			continue
		}
		if sanitizeRuntimeProtocol(&protocol) {
			result = append(result, protocol)
		}
	}
	return result
}

func normalizeProtocolType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "hysteria", "hysteria2":
		return "hysteria"
	case "ssr", "shadowsocks-r", "shadowsocksr":
		return "shadowsocksr"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func supportedRuntimeProtocol(protocol string) bool {
	switch protocol {
	case "shadowsocks", "shadowsocksr", "mieru", "hysteria", "anytls", "trojan", "vless", "vmess", "naive", "snell", "tuic":
		return true
	default:
		return false
	}
}

func normalizeProtocolNoopFields(protocol *Protocol) {
	protocol.Security = normalizeNone(protocol.Security)
	protocol.CertMode = normalizeNone(protocol.CertMode)
	protocol.Flow = normalizeNone(protocol.Flow)
	protocol.Obfs = normalizeNone(protocol.Obfs)
	protocol.Multiplex = normalizeMultiplex(protocol.Type, protocol.Multiplex)
	protocol.Encryption = normalizeNone(protocol.Encryption)
	if protocol.Security != "tls" {
		clearCertificate(protocol)
	}
	if protocol.Encryption == "" {
		clearEncryption(protocol)
	}
}

func normalizeNone(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "none" {
		return ""
	}
	return value
}

func normalizeDisabled(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "none", "false", "off", "disabled":
		return ""
	default:
		return value
	}
}

func normalizeMultiplex(protocolType, raw string) string {
	value := strings.TrimSpace(raw)
	if protocolType != "mieru" {
		return normalizeDisabled(value)
	}
	switch strings.ToUpper(value) {
	case "", "LOW", "MULTIPLEXING_DEFAULT", "MULTIPLEXING_LOW":
		return "MULTIPLEXING_LOW"
	case "NONE", "FALSE", "OFF", "DISABLED", "MULTIPLEXING_OFF":
		return "MULTIPLEXING_OFF"
	case "MIDDLE", "MEDIUM", "MULTIPLEXING_MIDDLE":
		return "MULTIPLEXING_MIDDLE"
	case "HIGH", "MULTIPLEXING_HIGH":
		return "MULTIPLEXING_HIGH"
	default:
		return value
	}
}

func sanitizeRuntimeProtocol(protocol *Protocol) bool {
	switch protocol.Type {
	case "shadowsocks":
		pluginTLS := shadowsocksPluginUsesTLS(protocol)
		if pluginTLS {
			protocol.Security = "tls"
		} else {
			protocol.Security = ""
			protocol.SNI = ""
			clearCertificate(protocol)
		}
		clearTLSClient(protocol)
		clearReality(protocol)
		clearLegacyObfs(protocol)
		clearStreamTransport(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		clearShadowsocksClientPluginOptions(protocol)
		return protocol.Port > 0 && protocol.Cipher != "" && (!pluginTLS || hasTLSCertificate(*protocol))
	case "shadowsocksr":
		protocol.Security = ""
		protocol.SNI = ""
		protocol.Multiplex = ""
		clearTLSClient(protocol)
		clearCertificate(protocol)
		clearReality(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && protocol.Cipher != "" && protocol.ServerKey != "" && protocol.SSRProtocol != ""
	case "mieru":
		protocol.Security = ""
		protocol.SNI = ""
		protocol.Multiplex = ""
		clearTLSClient(protocol)
		clearCertificate(protocol)
		clearReality(protocol)
		return protocol.Port > 0
	case "snell":
		protocol.Security = ""
		protocol.SNI = ""
		clearTLSClient(protocol)
		clearCertificate(protocol)
		clearReality(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		if protocol.Version == 0 {
			protocol.Version = 5
		}
		if protocol.Version == 5 {
			protocol.Mode = ""
		} else {
			protocol.Obfs = ""
		}
		return protocol.Port > 0 && protocol.ServerKey != "" && (protocol.Version == 5 || protocol.Version == 6)
	case "hysteria":
		protocol.Security = "tls"
		clearTLSClient(protocol)
		clearReality(protocol)
		clearStreamTransport(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && hasTLSCertificate(*protocol)
	case "naive":
		protocol.Security = "tls"
		clearTLSClient(protocol)
		clearReality(protocol)
		clearStreamTransport(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && hasTLSCertificate(*protocol)
	case "tuic":
		protocol.Security = "tls"
		protocol.DisableSNI = false
		protocol.UDPRelayMode = ""
		clearTLSClient(protocol)
		clearReality(protocol)
		clearStreamTransport(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && hasTLSCertificate(*protocol)
	case "anytls":
		clearTLSClient(protocol)
		clearStreamTransport(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		protocol.Multiplex = ""
		if protocol.Security == "tls" {
			clearReality(protocol)
			return protocol.Port > 0 && hasTLSCertificate(*protocol)
		}
		if protocol.Security == "reality" {
			clearCertificate(protocol)
			return protocol.Port > 0 && protocol.SNI != "" && protocol.RealityPrivateKey != "" && protocol.RealityShortId != ""
		}
		return false
	case "trojan":
		clearTLSClient(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		if protocol.Security == "tls" {
			clearReality(protocol)
			return protocol.Port > 0 && hasTLSCertificate(*protocol)
		}
		if protocol.Security == "reality" {
			clearCertificate(protocol)
			return protocol.Port > 0 && protocol.SNI != "" && protocol.RealityPrivateKey != "" && protocol.RealityShortId != ""
		}
		clearCertificate(protocol)
		clearReality(protocol)
		return protocol.Port > 0
	case "vless", "vmess":
		clearLegacyObfs(protocol)
		if protocol.Type != "vless" {
			protocol.Flow = ""
			clearEncryption(protocol)
		}
		if protocol.Security == "tls" {
			clearTLSClient(protocol)
			clearReality(protocol)
			return protocol.Port > 0 && hasTLSCertificate(*protocol)
		}
		if protocol.Security == "reality" {
			clearCertificate(protocol)
			return protocol.Port > 0 && protocol.SNI != "" && protocol.RealityPrivateKey != "" && protocol.RealityShortId != ""
		}
		clearTLSClient(protocol)
		protocol.SNI = ""
		clearCertificate(protocol)
		clearReality(protocol)
		return protocol.Port > 0
	default:
		return false
	}
}

func validateRuntimeProtocol(protocol *Protocol) error {
	switch protocol.Type {
	case "hysteria", "naive", "trojan", "tuic":
		if protocol.Security != "tls" {
			return fmt.Errorf("%s requires tls security", protocol.Type)
		}
	case "anytls":
		if protocol.Security != "tls" && protocol.Security != "reality" {
			return fmt.Errorf("%s requires tls or reality security", protocol.Type)
		}
	case "mieru":
		transport := strings.ToLower(strings.TrimSpace(protocol.Transport))
		if transport == "" {
			transport = strings.ToLower(strings.TrimSpace(protocol.Network))
		}
		if transport != "" && transport != "tcp" && transport != "udp" {
			return fmt.Errorf("mieru requires tcp or udp transport")
		}
		switch protocol.Multiplex {
		case "MULTIPLEXING_OFF", "MULTIPLEXING_LOW", "MULTIPLEXING_MIDDLE", "MULTIPLEXING_HIGH":
		default:
			return fmt.Errorf("mieru multiplex is invalid")
		}
	case "snell":
		if protocol.Version == 0 {
			protocol.Version = 5
		}
		protocol.Mode = strings.ToLower(strings.TrimSpace(protocol.Mode))
		protocol.Transport = strings.ToLower(strings.TrimSpace(protocol.Transport))
		protocol.Network = strings.ToLower(strings.TrimSpace(protocol.Network))
		if protocol.Version != 5 && protocol.Version != 6 {
			return fmt.Errorf("snell requires version 5 or 6")
		}
		if protocol.ServerKey == "" || protocol.Version == 6 && len(protocol.ServerKey) < 12 {
			return fmt.Errorf("snell requires valid server_key")
		}
		if protocol.Version == 5 {
			if protocol.Mode != "" {
				return fmt.Errorf("snell v5 does not support mode")
			}
			if protocol.Obfs != "" && protocol.Obfs != "http" && protocol.Obfs != "tls" {
				return fmt.Errorf("snell obfs is invalid")
			}
		} else {
			if protocol.Obfs != "" {
				return fmt.Errorf("snell v6 does not support obfs")
			}
			mode := strings.ToLower(strings.TrimSpace(protocol.Mode))
			if mode != "" && mode != "default" && mode != "unshaped" && mode != "unsafe-raw" {
				return fmt.Errorf("snell mode is invalid")
			}
			protocol.Mode = mode
		}
		if protocol.Transport != "" && protocol.Transport != "tcp" || protocol.Network != "" && protocol.Network != "tcp" {
			return fmt.Errorf("snell requires tcp transport")
		}
	case "shadowsocksr":
		transport := strings.ToLower(strings.TrimSpace(protocol.Transport))
		if transport == "" {
			transport = strings.ToLower(strings.TrimSpace(protocol.Network))
		}
		switch transport {
		case "", "both", "tcp,udp", "tcp+udp", "tcp", "udp":
		default:
			return fmt.Errorf("shadowsocksr network is invalid")
		}
		protocol.Transport = transport
		protocol.Network = ""
		protocol.Cipher = strings.ToLower(strings.TrimSpace(protocol.Cipher))
		protocol.SSRProtocol = strings.ToLower(strings.TrimSpace(protocol.SSRProtocol))
		protocol.Obfs = strings.ToLower(strings.TrimSpace(protocol.Obfs))
		if !validShadowsocksrCipher(protocol.Cipher) {
			return fmt.Errorf("shadowsocksr cipher is invalid")
		}
		if protocol.ServerKey == "" {
			return fmt.Errorf("shadowsocksr requires server_key")
		}
		if !validShadowsocksrProtocol(protocol.SSRProtocol) {
			return fmt.Errorf("shadowsocksr protocol is invalid")
		}
		if protocol.Obfs == "" {
			protocol.Obfs = "plain"
		}
		if !validShadowsocksrObfs(protocol.Obfs) {
			return fmt.Errorf("shadowsocksr obfs is invalid")
		}
	}
	if protocolRequiresTLSCertificate(*protocol) && !hasTLSCertificate(*protocol) {
		return fmt.Errorf("%s requires sni and cert_mode", protocol.Type)
	}
	if protocol.Type == "vless" && protocol.Flow == "xtls-rprx-vision" &&
		protocol.Security != "tls" && protocol.Security != "reality" {
		return fmt.Errorf("vless vision requires tls or reality security")
	}
	if protocol.Type == "shadowsocks" && (protocol.Obfs != "" || protocol.ObfsHost != "" ||
		protocol.ObfsPath != "" || protocol.ObfsPassword != "") {
		return fmt.Errorf("shadowsocks legacy obfs fields are unsupported")
	}
	if protocol.Type == "shadowsocks" {
		if err := validateShadowsocksPlugin(protocol); err != nil {
			return err
		}
	}
	return nil
}

func normalizeShadowsocksPluginName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "none", "false", "off", "disabled":
		return ""
	case "obfs", "obfs-local", "obfs-server", "simple-obfs":
		return "obfs"
	case "v2ray-plugin":
		return "v2ray-plugin"
	case "gost-plugin":
		return "gost-plugin"
	case "shadow-tls", "shadowtls":
		return "shadow-tls"
	case "restls", "res-tls":
		return "restls"
	case "kcptun", "kcp-tun":
		return "kcptun"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func validateShadowsocksPlugin(protocol *Protocol) error {
	options, err := newShadowsocksPluginValues(protocol.Plugin, protocol.PluginOptions)
	if err != nil {
		return err
	}

	if protocol.Plugin == "" {
		if len(options.values) > 0 {
			return fmt.Errorf("shadowsocks plugin_opts requires plugin")
		}
		protocol.PluginOptions = nil
		clearShadowsocksTLS(protocol)
		return nil
	}

	normalized, pluginTLS, err := normalizeShadowsocksPluginOptions(options)
	if err != nil {
		return err
	}
	if err := options.finish(); err != nil {
		return err
	}

	protocol.PluginOptions = normalized
	if pluginTLS {
		if protocol.Security != "tls" || !hasTLSCertificate(*protocol) {
			return fmt.Errorf("shadowsocks plugin tls requires sni and cert_mode")
		}
	} else {
		clearShadowsocksTLS(protocol)
	}
	return nil
}

func normalizeShadowsocksPluginOptions(options *shadowsocksPluginValues) (map[string]any, bool, error) {
	switch options.plugin {
	case "obfs":
		mode, err := options.requiredString("mode", "obfs")
		mode = strings.ToLower(strings.TrimSpace(mode))
		if err != nil || mode != "http" && mode != "tls" {
			return nil, false, options.invalid("mode")
		}
		normalized := map[string]any{"mode": mode}
		if host, exists, err := options.optionalString("host"); err != nil {
			return nil, false, err
		} else if exists && strings.TrimSpace(host) != "" {
			normalized["host"] = strings.TrimSpace(host)
		}
		return normalized, false, nil
	case "v2ray-plugin", "gost-plugin":
		return normalizeShadowsocksWebSocketPlugin(options)
	case "shadow-tls":
		return normalizeShadowTLSPlugin(options)
	case "restls":
		return normalizeRestlsPlugin(options)
	case "kcptun":
		return normalizeKcptunPlugin(options)
	default:
		return nil, false, fmt.Errorf("unsupported shadowsocks plugin: %s", options.plugin)
	}
}

func normalizeShadowsocksWebSocketPlugin(options *shadowsocksPluginValues) (map[string]any, bool, error) {
	mode, err := options.requiredString("mode")
	if err != nil || !strings.EqualFold(strings.TrimSpace(mode), "websocket") {
		return nil, false, options.invalid("mode")
	}
	normalized := map[string]any{"mode": "websocket"}
	if host, exists, err := options.optionalString("host"); err != nil {
		return nil, false, err
	} else if exists && strings.TrimSpace(host) != "" {
		normalized["host"] = strings.TrimSpace(host)
	}
	if path, exists, err := options.optionalString("path"); err != nil {
		return nil, false, err
	} else if exists && strings.TrimSpace(path) != "" {
		if !strings.HasPrefix(strings.TrimSpace(path), "/") {
			return nil, false, options.invalid("path")
		}
		normalized["path"] = strings.TrimSpace(path)
	}
	pluginTLS, exists, err := options.optionalBoolean("tls")
	if err != nil {
		return nil, false, err
	}
	if exists {
		normalized["tls"] = pluginTLS
	}
	if mux, exists, err := options.optionalBoolean("mux"); err != nil {
		return nil, false, err
	} else if exists {
		normalized["mux"] = mux
	}
	if options.plugin == "v2ray-plugin" {
		if upgrade, exists, err := options.optionalBoolean("v2ray-http-upgrade"); err != nil {
			return nil, false, err
		} else if exists {
			normalized["v2ray-http-upgrade"] = upgrade
		}
	}
	if headers, exists, err := options.optionalStringMap("headers"); err != nil {
		return nil, false, err
	} else if exists {
		normalized["headers"] = headers
	}
	return normalized, pluginTLS, nil
}

func normalizeShadowTLSPlugin(options *shadowsocksPluginValues) (map[string]any, bool, error) {
	normalized := make(map[string]any)
	version := 2
	if value, exists, err := options.optionalInteger("version"); err != nil {
		return nil, false, err
	} else if exists {
		version = value
		normalized["version"] = value
	}
	if version < 1 || version > 3 {
		return nil, false, options.invalid("version")
	}
	if password, exists, err := options.optionalString("password"); err != nil {
		return nil, false, err
	} else if exists && strings.TrimSpace(password) != "" {
		normalized["password"] = password
	} else if version > 1 {
		return nil, false, options.invalid("password")
	}
	if strict, exists, err := options.optionalBoolean("strict-mode", "strict_mode", "strict"); err != nil {
		return nil, false, err
	} else if exists {
		normalized["strict-mode"] = strict
	}
	handshake, err := options.requiredString("handshake", "dest")
	if err != nil || !validPluginAddress(handshake) {
		return nil, false, options.invalid("handshake")
	}
	normalized["handshake"] = handshake
	return normalized, false, nil
}

func normalizeRestlsPlugin(options *shadowsocksPluginValues) (map[string]any, bool, error) {
	normalized := make(map[string]any)
	password, err := options.requiredString("password")
	if err != nil {
		return nil, false, err
	}
	normalized["password"] = password
	if script, exists, err := options.optionalString("restls-script", "restls_script"); err != nil {
		return nil, false, err
	} else if exists && strings.TrimSpace(script) != "" {
		normalized["restls-script"] = script
	}
	dest, err := options.requiredString("dest", "handshake")
	if err != nil || !validPluginAddress(dest) {
		return nil, false, options.invalid("dest")
	}
	normalized["dest"] = dest
	if minRecordLen, exists, err := options.optionalInteger("min-record-len", "min_record_len"); err != nil {
		return nil, false, err
	} else if exists {
		if minRecordLen < 0 {
			return nil, false, options.invalid("min-record-len")
		}
		normalized["min-record-len"] = minRecordLen
	}
	return normalized, false, nil
}

func normalizeKcptunPlugin(options *shadowsocksPluginValues) (map[string]any, bool, error) {
	normalized := make(map[string]any)
	if key, exists, err := options.optionalString("key"); err != nil {
		return nil, false, err
	} else if exists && strings.TrimSpace(key) != "" {
		normalized["key"] = key
	}
	if crypt, exists, err := options.optionalString("crypt"); err != nil {
		return nil, false, err
	} else if exists {
		crypt = strings.ToLower(strings.TrimSpace(crypt))
		if !validKcptunCrypt(crypt) {
			return nil, false, options.invalid("crypt")
		}
		normalized["crypt"] = crypt
	}
	if mode, exists, err := options.optionalString("mode"); err != nil {
		return nil, false, err
	} else if exists {
		mode = strings.ToLower(strings.TrimSpace(mode))
		if !validKcptunMode(mode) {
			return nil, false, options.invalid("mode")
		}
		normalized["mode"] = mode
	}
	for _, item := range kcptunIntegerOptions() {
		value, exists, err := options.optionalInteger(item.field)
		if err != nil {
			return nil, false, err
		}
		if !exists {
			continue
		}
		if value < item.min || item.max > 0 && value > item.max {
			return nil, false, options.invalid(item.field)
		}
		normalized[item.field] = value
	}
	if nocomp, exists, err := options.optionalBoolean("nocomp"); err != nil {
		return nil, false, err
	} else if exists {
		normalized["nocomp"] = nocomp
	}
	if acknodelay, exists, err := options.optionalBoolean("acknodelay"); err != nil {
		return nil, false, err
	} else if exists {
		normalized["acknodelay"] = acknodelay
	}
	return normalized, false, nil
}

func shadowsocksPluginUsesTLS(protocol *Protocol) bool {
	if protocol.Type != "shadowsocks" || protocol.Plugin != "v2ray-plugin" && protocol.Plugin != "gost-plugin" {
		return false
	}
	options, err := newShadowsocksPluginValues(protocol.Plugin, protocol.PluginOptions)
	if err != nil {
		return false
	}
	pluginTLS, _, err := options.optionalBoolean("tls")
	return err == nil && pluginTLS
}

func clearShadowsocksTLS(protocol *Protocol) {
	protocol.Security = ""
	protocol.SNI = ""
	clearCertificate(protocol)
	clearTLSClient(protocol)
	clearReality(protocol)
}

func clearShadowsocksClientPluginOptions(protocol *Protocol) {
	if protocol.Plugin != "obfs" {
		return
	}
	if options, ok := protocol.PluginOptions.(map[string]any); ok {
		delete(options, "host")
	}
}

func validPluginAddress(address string) bool {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || strings.TrimSpace(host) == "" {
		return false
	}
	parsed, err := strconv.ParseUint(port, 10, 16)
	return err == nil && parsed > 0
}

func validKcptunCrypt(crypt string) bool {
	switch crypt {
	case "null", "tea", "xor", "none", "aes-128", "aes-192", "blowfish", "twofish",
		"cast5", "3des", "xtea", "salsa20", "aes-128-gcm", "aes":
		return true
	default:
		return false
	}
}

func validKcptunMode(mode string) bool {
	switch mode {
	case "normal", "fast", "fast2", "fast3", "manual":
		return true
	default:
		return false
	}
}

func validShadowsocksrCipher(cipher string) bool {
	switch strings.ToLower(strings.TrimSpace(cipher)) {
	case "none", "aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb", "rc4-md5", "chacha20", "chacha20-ietf":
		return true
	default:
		return false
	}
}

func validShadowsocksrProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "auth_aes128_md5", "auth_aes128_sha1", "auth_chain_a":
		return true
	default:
		return false
	}
}

func validShadowsocksrObfs(obfs string) bool {
	switch strings.ToLower(strings.TrimSpace(obfs)) {
	case "plain", "http_simple", "http_post", "tls1.2_ticket_auth", "tls1.2_ticket_fastauth":
		return true
	default:
		return false
	}
}

type kcptunIntegerOption struct {
	field string
	min   int
	max   int
}

func kcptunIntegerOptions() []kcptunIntegerOption {
	return []kcptunIntegerOption{
		{field: "conn", min: 1}, {field: "autoexpire", min: 0},
		{field: "scavengettl", min: 0}, {field: "mtu", min: 576},
		{field: "ratelimit", min: 0}, {field: "sndwnd", min: 1},
		{field: "rcvwnd", min: 1}, {field: "datashard", min: 1},
		{field: "parityshard", min: 1}, {field: "dscp", min: 0, max: 63},
		{field: "nodelay", min: 0}, {field: "interval", min: 0},
		{field: "resend", min: 0}, {field: "nc", min: 0},
		{field: "sockbuf", min: 1}, {field: "smuxver", min: 1, max: 2},
		{field: "smuxbuf", min: 1}, {field: "framesize", min: 1, max: 65535},
		{field: "streambuf", min: 1}, {field: "keepalive", min: 1},
	}
}

type shadowsocksPluginValues struct {
	plugin string
	values map[string]any
}

func newShadowsocksPluginValues(plugin string, raw any) (*shadowsocksPluginValues, error) {
	values := make(map[string]any)
	switch typed := raw.(type) {
	case nil:
	case map[string]any:
		for key, value := range typed {
			values[key] = value
		}
	case map[string]string:
		for key, value := range typed {
			values[key] = value
		}
	default:
		return nil, fmt.Errorf("shadowsocks plugin_opts is invalid")
	}
	return &shadowsocksPluginValues{plugin: plugin, values: values}, nil
}

func (values *shadowsocksPluginValues) take(field string, aliases ...string) (any, bool, error) {
	keys := append([]string{field}, aliases...)
	var result any
	found := false
	for _, key := range keys {
		value, exists := values.values[key]
		if !exists {
			continue
		}
		delete(values.values, key)
		if found {
			return nil, false, values.invalid(field)
		}
		result = value
		found = true
	}
	return result, found, nil
}

func (values *shadowsocksPluginValues) requiredString(field string, aliases ...string) (string, error) {
	text, exists, err := values.optionalString(field, aliases...)
	if err != nil {
		return "", err
	}
	if !exists || strings.TrimSpace(text) == "" {
		return "", values.invalid(field)
	}
	return text, nil
}

func (values *shadowsocksPluginValues) optionalString(field string, aliases ...string) (string, bool, error) {
	value, exists, err := values.take(field, aliases...)
	if err != nil || !exists {
		return "", exists, err
	}
	text, ok := value.(string)
	if !ok {
		return "", false, values.invalid(field)
	}
	return text, true, nil
}

func (values *shadowsocksPluginValues) optionalBoolean(field string, aliases ...string) (bool, bool, error) {
	value, exists, err := values.take(field, aliases...)
	if err != nil || !exists {
		return false, exists, err
	}
	switch typed := value.(type) {
	case bool:
		return typed, true, nil
	case string:
		parsed, parseErr := strconv.ParseBool(typed)
		if parseErr == nil {
			return parsed, true, nil
		}
	}
	return false, false, values.invalid(field)
}

func (values *shadowsocksPluginValues) optionalInteger(field string, aliases ...string) (int, bool, error) {
	value, exists, err := values.take(field, aliases...)
	if err != nil || !exists {
		return 0, exists, err
	}
	var number int64
	switch typed := value.(type) {
	case json.Number:
		number, err = typed.Int64()
	case int:
		return typed, true, nil
	case int64:
		number = typed
	case float64:
		number = int64(typed)
		if float64(number) != typed {
			err = fmt.Errorf("fractional integer")
		}
	case string:
		number, err = strconv.ParseInt(typed, 10, 32)
	default:
		err = fmt.Errorf("invalid integer")
	}
	if err != nil || int64(int(number)) != number {
		return 0, false, values.invalid(field)
	}
	return int(number), true, nil
}

func (values *shadowsocksPluginValues) optionalStringMap(field string) (map[string]string, bool, error) {
	value, exists, err := values.take(field)
	if err != nil || !exists {
		return nil, exists, err
	}
	if typed, ok := value.(map[string]string); ok {
		return typed, true, nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil, false, values.invalid(field)
	}
	result := make(map[string]string, len(typed))
	for key, item := range typed {
		text, ok := item.(string)
		if !ok {
			return nil, false, values.invalid(field)
		}
		result[key] = text
	}
	return result, true, nil
}

func (values *shadowsocksPluginValues) finish() error {
	for key := range values.values {
		return values.invalid(key)
	}
	return nil
}

func (values *shadowsocksPluginValues) invalid(field string) error {
	return fmt.Errorf("shadowsocks plugin %s option %s is invalid", values.plugin, field)
}

func protocolRequiresTLSCertificate(protocol Protocol) bool {
	if protocol.Security != "tls" {
		return false
	}
	switch protocol.Type {
	case "anytls", "hysteria", "naive", "trojan", "tuic", "vless", "vmess":
		return true
	default:
		return false
	}
}

func hasTLSCertificate(protocol Protocol) bool {
	return protocol.SNI != "" && protocol.CertMode != ""
}

func clearTLSClient(protocol *Protocol) {
	protocol.AllowInsecure = false
	protocol.Fingerprint = ""
}

func clearCertificate(protocol *Protocol) {
	protocol.CertMode = ""
	protocol.CertDNSProvider = ""
	protocol.CertDNSEnv = ""
}

func clearReality(protocol *Protocol) {
	protocol.RealityServerAddr = ""
	protocol.RealityServerPort = 0
	protocol.RealityPrivateKey = ""
	protocol.RealityPublicKey = ""
	protocol.RealityShortId = ""
}

func clearStreamTransport(protocol *Protocol) {
	protocol.Transport = ""
	protocol.Host = ""
	protocol.Path = ""
	protocol.ServiceName = ""
	protocol.XhttpMode = ""
	protocol.XhttpExtra = ""
}

func clearLegacyObfs(protocol *Protocol) {
	protocol.Obfs = ""
	protocol.ObfsHost = ""
	protocol.ObfsPath = ""
	protocol.ObfsPassword = ""
}

func clearQUICControls(protocol *Protocol) {
	protocol.HopPorts = ""
	protocol.HopInterval = 0
	protocol.UDPRelayMode = ""
	protocol.CongestionController = ""
	protocol.QUICCongestionControl = ""
	protocol.ReduceRtt = false
	protocol.Heartbeat = 0
	protocol.Multiplex = ""
	protocol.PaddingScheme = ""
}

func clearEncryption(protocol *Protocol) {
	protocol.EncryptionMode = ""
	protocol.EncryptionRtt = ""
	protocol.EncryptionTicket = ""
	protocol.EncryptionServerPadding = ""
	protocol.EncryptionPrivateKey = ""
	protocol.EncryptionClientPadding = ""
	protocol.EncryptionPassword = ""
}
