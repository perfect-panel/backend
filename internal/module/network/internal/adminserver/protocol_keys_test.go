package adminserver

import (
	"testing"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

func TestEnsureGeneratedProtocolKeyGeneratesForEmptySSR(t *testing.T) {
	for _, protocolType := range []string{"shadowsocksr", "ssr"} {
		t.Run(protocolType, func(t *testing.T) {
			protocol := node.Protocol{Type: protocolType}
			ensureGeneratedProtocolKey(&protocol, nil)
			if len(protocol.ServerKey) != generatedServerKeyLength {
				t.Fatalf("ServerKey length = %d, want %d", len(protocol.ServerKey), generatedServerKeyLength)
			}
		})
	}
}

func TestEnsureGeneratedProtocolKeySkipsSnell(t *testing.T) {
	protocol := node.Protocol{Type: "snell"}
	ensureGeneratedProtocolKey(&protocol, nil)
	if protocol.ServerKey != "" {
		t.Fatalf("ServerKey = %q, want empty for snell", protocol.ServerKey)
	}
}

func TestEnsureGeneratedProtocolKeyPreservesExistingKey(t *testing.T) {
	protocol := node.Protocol{Type: "shadowsocksr"}
	ensureGeneratedProtocolKey(&protocol, map[string]string{"shadowsocksr": "existing-psk"})
	if protocol.ServerKey != "existing-psk" {
		t.Fatalf("ServerKey = %q, want existing-psk", protocol.ServerKey)
	}
}

func TestEnsureGeneratedProtocolKeyKeepsProvidedKey(t *testing.T) {
	protocol := node.Protocol{Type: "shadowsocksr", ServerKey: "provided"}
	ensureGeneratedProtocolKey(&protocol, map[string]string{"shadowsocksr": "existing"})
	if protocol.ServerKey != "provided" {
		t.Fatalf("ServerKey = %q, want provided", protocol.ServerKey)
	}
}

func TestProtocolKeyLookupNormalizesAliases(t *testing.T) {
	keys := protocolKeyLookup([]node.Protocol{{Type: "ssr", ServerKey: "secret"}})
	if keys["shadowsocksr"] != "secret" {
		t.Fatalf("SSR key = %q, want secret", keys["shadowsocksr"])
	}
}

func TestEnsureShadowsocks2022ServerKeyPreservesExistingKey(t *testing.T) {
	protocol := node.Protocol{Type: "shadowsocks", Cipher: "2022-blake3-aes-128-gcm"}
	ensureShadowsocks2022ServerKey(&protocol, map[string]string{"shadowsocks": "1234567890abcdef"})
	if protocol.ServerKey != "1234567890abcdef" {
		t.Fatalf("ServerKey = %q, want existing key", protocol.ServerKey)
	}
}

func TestEnsureRealityProtocolKeyPreservesExistingKey(t *testing.T) {
	protocol := node.Protocol{Type: "vless", Security: "reality"}
	ensureErr := ensureRealityProtocolKey(&protocol, map[string]realityProtocolKey{
		"vless": {
			privateKey: "existing-private",
			publicKey:  "existing-public",
			shortID:    "existing-short-id",
		},
	})
	if ensureErr != nil {
		t.Fatalf("ensureRealityProtocolKey() error = %v", ensureErr)
	}
	if protocol.RealityPrivateKey != "existing-private" ||
		protocol.RealityPublicKey != "existing-public" ||
		protocol.RealityShortId != "existing-short-id" {
		t.Fatalf("Reality keys were not preserved: %#v", protocol)
	}
}

func TestEnsureRealityProtocolKeyKeepsProvidedCompleteKey(t *testing.T) {
	protocol := node.Protocol{
		Type:              "vless",
		Security:          "reality",
		RealityPrivateKey: "provided-private",
		RealityPublicKey:  "provided-public",
		RealityShortId:    "provided-short-id",
	}
	ensureErr := ensureRealityProtocolKey(&protocol, map[string]realityProtocolKey{
		"vless": {
			privateKey: "existing-private",
			publicKey:  "existing-public",
			shortID:    "existing-short-id",
		},
	})
	if ensureErr != nil {
		t.Fatalf("ensureRealityProtocolKey() error = %v", ensureErr)
	}
	if protocol.RealityPrivateKey != "provided-private" ||
		protocol.RealityPublicKey != "provided-public" ||
		protocol.RealityShortId != "provided-short-id" {
		t.Fatalf("Reality keys were overwritten: %#v", protocol)
	}
}

func TestEnsureRealityProtocolKeyGeneratesForNewReality(t *testing.T) {
	for _, protocolType := range []string{"vless", "vmess", "trojan", "anytls"} {
		t.Run(protocolType, func(t *testing.T) {
			protocol := node.Protocol{Type: protocolType, Security: "reality"}
			if err := ensureRealityProtocolKey(&protocol, nil); err != nil {
				t.Fatalf("ensureRealityProtocolKey() error = %v", err)
			}
			if protocol.RealityPrivateKey == "" || protocol.RealityPublicKey == "" ||
				protocol.RealityShortId == "" {
				t.Fatalf("Reality keys were not generated: %#v", protocol)
			}
		})
	}
}

func TestEnsureRealityProtocolKeySkipsNonRealitySecurity(t *testing.T) {
	protocol := node.Protocol{Type: "trojan", Security: "tls"}
	if err := ensureRealityProtocolKey(&protocol, nil); err != nil {
		t.Fatalf("ensureRealityProtocolKey() error = %v", err)
	}
	if protocol.RealityPrivateKey != "" || protocol.RealityPublicKey != "" ||
		protocol.RealityShortId != "" {
		t.Fatalf("Reality keys were generated for tls security: %#v", protocol)
	}
}

func TestMergeMissingProtocolFieldsPreservesUnsubmittedFields(t *testing.T) {
	existing := node.Protocol{
		Type:              "vless",
		Port:              443,
		Enable:            true,
		Security:          "reality",
		SNI:               "old.example.com",
		RealityPrivateKey: "existing-private",
		RealityPublicKey:  "existing-public",
		RealityShortId:    "existing-short-id",
	}
	next := node.Protocol{
		Type:     "vless",
		Port:     8443,
		Enable:   true,
		Security: "reality",
		SNI:      "new.example.com",
	}
	merged, err := mergeMissingProtocolFields(next, existing, map[string]struct{}{
		"type":     {},
		"port":     {},
		"enable":   {},
		"security": {},
		"sni":      {},
	})
	if err != nil {
		t.Fatalf("mergeMissingProtocolFields() error = %v", err)
	}
	if merged.RealityPrivateKey != "existing-private" ||
		merged.RealityPublicKey != "existing-public" ||
		merged.RealityShortId != "existing-short-id" {
		t.Fatalf("Reality keys were not merged: %#v", merged)
	}
	if merged.Port != 8443 || merged.SNI != "new.example.com" {
		t.Fatalf("submitted fields were not preserved: %#v", merged)
	}
}

func TestMergeMissingProtocolFieldsAllowsExplicitClear(t *testing.T) {
	existing := node.Protocol{
		Type:              "vless",
		Security:          "reality",
		RealityPrivateKey: "existing-private",
		RealityPublicKey:  "existing-public",
		RealityShortId:    "existing-short-id",
	}
	next := node.Protocol{Type: "vless", Security: "reality"}
	merged, err := mergeMissingProtocolFields(next, existing, map[string]struct{}{
		"type":                {},
		"security":            {},
		"reality_private_key": {},
	})
	if err != nil {
		t.Fatalf("mergeMissingProtocolFields() error = %v", err)
	}
	if merged.RealityPrivateKey != "" {
		t.Fatalf("RealityPrivateKey = %q, want explicit clear", merged.RealityPrivateKey)
	}
	if merged.RealityPublicKey != "existing-public" || merged.RealityShortId != "existing-short-id" {
		t.Fatalf("unsubmitted Reality fields were not preserved: %#v", merged)
	}
}

// The update binds the body case-insensitively, so a key in another case
// still sets its field; the merge must not put the stored value back.
func TestMergeMissingProtocolFieldsMatchesKeysCaseInsensitively(t *testing.T) {
	existing := node.Protocol{Type: "vless", Enable: true, Security: "reality"}
	next := node.Protocol{Type: "vless", Enable: false}
	merged, err := mergeMissingProtocolFields(next, existing, map[string]struct{}{
		"Type":   {},
		"Enable": {},
	})
	if err != nil {
		t.Fatalf("mergeMissingProtocolFields() error = %v", err)
	}
	if merged.Enable {
		t.Fatalf("Enable = true, want the submitted false")
	}
	if merged.Security != "reality" {
		t.Fatalf("Security = %q, want the unsubmitted stored value", merged.Security)
	}
}

func TestNormalizeAfterMergeClearsRealityWhenSecurityChanges(t *testing.T) {
	existing := node.Protocol{
		Type:              "vless",
		Port:              443,
		Enable:            true,
		Security:          "reality",
		SNI:               "old.example.com",
		RealityPrivateKey: "existing-private",
		RealityPublicKey:  "existing-public",
		RealityShortId:    "existing-short-id",
	}
	next := node.Protocol{
		Type:     "vless",
		Port:     443,
		Enable:   true,
		Security: "tls",
		SNI:      "new.example.com",
		CertMode: "self",
	}
	merged, err := mergeMissingProtocolFields(next, existing, map[string]struct{}{
		"type":      {},
		"port":      {},
		"enable":    {},
		"security":  {},
		"sni":       {},
		"cert_mode": {},
	})
	if err != nil {
		t.Fatalf("mergeMissingProtocolFields() error = %v", err)
	}
	normalized, err := node.NormalizeProtocolForStorage(merged)
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if normalized.RealityPrivateKey != "" || normalized.RealityPublicKey != "" || normalized.RealityShortId != "" {
		t.Fatalf("Reality keys were not cleared after security changed: %#v", normalized)
	}
}

func TestNormalizeAfterMergeClearsPluginOptionsWhenPluginDisabled(t *testing.T) {
	existing := node.Protocol{
		Type:          "shadowsocks",
		Port:          8388,
		Enable:        true,
		Cipher:        "chacha20-ietf-poly1305",
		Plugin:        "obfs",
		PluginOptions: map[string]any{"mode": "http", "host": "old.example.com"},
	}
	next := node.Protocol{
		Type:   "shadowsocks",
		Port:   8388,
		Enable: true,
		Cipher: "chacha20-ietf-poly1305",
		Plugin: "none",
	}
	merged, err := mergeMissingProtocolFields(next, existing, map[string]struct{}{
		"type":   {},
		"port":   {},
		"enable": {},
		"cipher": {},
		"plugin": {},
	})
	if err != nil {
		t.Fatalf("mergeMissingProtocolFields() error = %v", err)
	}
	normalized, err := node.NormalizeProtocolForStorage(merged)
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if normalized.Plugin != "" || normalized.PluginOptions != nil {
		t.Fatalf("plugin fields were not cleared: %#v", normalized)
	}
}

func TestMergeMissingProtocolFieldsPreservesNodeReportedCertPin(t *testing.T) {
	// Admin clients never submit cert_pin_sha256 (the field is absent from the
	// dto), so an admin edit must carry the node-reported value forward.
	const pin = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	existing := node.Protocol{
		Type:          "vmess",
		Port:          443,
		Enable:        true,
		Security:      "tls",
		SNI:           "node.example",
		CertMode:      "self",
		CertPinSHA256: pin,
	}
	next := node.Protocol{
		Type:     "vmess",
		Port:     8443,
		Enable:   true,
		Security: "tls",
		SNI:      "node.example",
		CertMode: "self",
	}
	merged, err := mergeMissingProtocolFields(next, existing, map[string]struct{}{
		"type": {}, "port": {}, "enable": {}, "security": {}, "sni": {}, "cert_mode": {},
	})
	if err != nil {
		t.Fatalf("mergeMissingProtocolFields() error = %v", err)
	}
	if merged.CertPinSHA256 != pin {
		t.Fatalf("CertPinSHA256 = %q, want %q", merged.CertPinSHA256, pin)
	}
	normalized, err := node.NormalizeProtocolForStorage(merged)
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if normalized.CertPinSHA256 != pin {
		t.Fatalf("normalized CertPinSHA256 = %q, want %q", normalized.CertPinSHA256, pin)
	}
}
