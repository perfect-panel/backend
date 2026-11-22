package render

import (
	"net"
	"strings"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

func clientPluginConfig(protocol node.Protocol, server string) (string, map[string]any) {
	name := normalizePluginName(protocol.Plugin)
	options := pluginOptionMap(protocol.PluginOptions)
	switch name {
	case "obfs":
		options["host"] = firstNonEmpty(protocol.SNI, server)
	case "v2ray-plugin", "gost-plugin":
		applyPluginTLSOptions(options, protocol)
	case "shadow-tls":
		handshake := takeString(options, "handshake", "dest")
		delete(options, "strict-mode")
		delete(options, "strict_mode")
		delete(options, "strict")
		options["host"] = firstNonEmpty(addressHost(handshake), protocol.SNI, server)
		applyPluginTLSOptions(options, protocol)
		if len(protocol.ALPN) > 0 {
			options["alpn"] = protocol.ALPN
		}
	case "restls":
		destination := takeString(options, "dest", "handshake")
		delete(options, "min-record-len")
		delete(options, "min_record_len")
		options["host"] = firstNonEmpty(addressHost(destination), protocol.SNI, server)
		applyPluginTLSOptions(options, protocol)
	}
	return name, options
}

func normalizePluginName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "obfs-local", "obfs-server", "simple-obfs":
		return "obfs"
	case "shadowtls":
		return "shadow-tls"
	case "res-tls":
		return "restls"
	case "kcp-tun":
		return "kcptun"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func pluginOptionMap(raw any) map[string]any {
	result := make(map[string]any)
	switch options := raw.(type) {
	case map[string]any:
		for key, value := range options {
			result[key] = value
		}
	case map[string]string:
		for key, value := range options {
			result[key] = value
		}
	}
	return result
}

func applyPluginTLSOptions(options map[string]any, protocol node.Protocol) {
	if protocol.Fingerprint != "" {
		options["fingerprint"] = protocol.Fingerprint
	}
	if protocol.AllowInsecure {
		options["skip-cert-verify"] = true
	}
}

func takeString(options map[string]any, keys ...string) string {
	var result string
	for _, key := range keys {
		value, exists := options[key]
		delete(options, key)
		if text, ok := value.(string); result == "" && exists && ok && strings.TrimSpace(text) != "" {
			result = text
		}
	}
	return result
}

func addressHost(address string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err == nil {
		return host
	}
	return strings.Trim(strings.TrimSpace(address), "[]")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
