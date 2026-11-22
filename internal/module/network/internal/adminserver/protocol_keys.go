package adminserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

const (
	generatedServerKeyLength = 32
	// generatedServerKeySeedBytes is how much randomness a generated server
	// key is derived from.
	generatedServerKeySeedBytes = 32
)

// randomServerKeySeed returns the random seed of a generated server key,
// hex-encoded. The key was derived from a UUIDv7 before: at most 74 random
// bits, and the time it was issued in the rest.
func randomServerKeySeed() string {
	seed := make([]byte, generatedServerKeySeedBytes)
	// crypto/rand.Read never returns an error (Go 1.24 and later).
	_, _ = rand.Read(seed)
	return hex.EncodeToString(seed)
}

type realityProtocolKey struct {
	privateKey string
	publicKey  string
	shortID    string
}

func protocolLookup(protocols []node.Protocol) map[string]node.Protocol {
	lookup := make(map[string]node.Protocol, len(protocols))
	for _, protocol := range protocols {
		protocolType := normalizedProtocolType(protocol.Type)
		if protocolType == "" {
			continue
		}
		lookup[protocolType] = protocol
	}
	return lookup
}

func protocolFieldSetAt(fieldSets []map[string]struct{}, index int) map[string]struct{} {
	if index < 0 || index >= len(fieldSets) {
		return nil
	}
	return fieldSets[index]
}

func mergeMissingProtocolFields(next node.Protocol, existing node.Protocol, provided map[string]struct{}) (node.Protocol, error) {
	if len(provided) == 0 {
		return next, nil
	}
	existingMap, err := protocolJSONMap(existing)
	if err != nil {
		return node.Protocol{}, err
	}
	nextMap, err := protocolJSONMap(next)
	if err != nil {
		return node.Protocol{}, err
	}
	for field, value := range existingMap {
		if !providedField(provided, field) {
			nextMap[field] = value
		}
	}
	data, err := json.Marshal(nextMap)
	if err != nil {
		return node.Protocol{}, err
	}
	var merged node.Protocol
	if err := json.Unmarshal(data, &merged); err != nil {
		return node.Protocol{}, err
	}
	return merged, nil
}

// providedField reports whether the request named field. The request body
// is decoded case-insensitively, like encoding/json, so a key sent as
// "Enable" set the enable field and counts as provided.
func providedField(provided map[string]struct{}, field string) bool {
	if _, ok := provided[field]; ok {
		return true
	}
	for key := range provided {
		if strings.EqualFold(key, field) {
			return true
		}
	}
	return false
}

func protocolJSONMap(protocol node.Protocol) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(protocol)
	if err != nil {
		return nil, err
	}
	values := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func protocolKeyLookup(protocols []node.Protocol) map[string]string {
	keys := make(map[string]string, len(protocols))
	for _, protocol := range protocols {
		protocolType := generatedKeyProtocolType(protocol.Type)
		if protocolType == "" || strings.TrimSpace(protocol.ServerKey) == "" {
			continue
		}
		keys[protocolType] = protocol.ServerKey
	}
	return keys
}

func serverKeyLookup(protocols []node.Protocol) map[string]string {
	keys := make(map[string]string, len(protocols))
	for _, protocol := range protocols {
		protocolType := normalizedProtocolType(protocol.Type)
		if protocolType == "" || strings.TrimSpace(protocol.ServerKey) == "" {
			continue
		}
		keys[protocolType] = protocol.ServerKey
	}
	return keys
}

func ensureGeneratedProtocolKey(protocol *node.Protocol, existing map[string]string) {
	protocolType := generatedKeyProtocolType(protocol.Type)
	if protocolType == "" || strings.TrimSpace(protocol.ServerKey) != "" {
		return
	}
	if key := strings.TrimSpace(existing[protocolType]); key != "" {
		protocol.ServerKey = key
		return
	}
	protocol.ServerKey = protocolkey.GenerateCipher(randomServerKeySeed(), generatedServerKeyLength)
}

func ensureShadowsocks2022ServerKey(protocol *node.Protocol, existing map[string]string) {
	if normalizedProtocolType(protocol.Type) != "shadowsocks" || !strings.Contains(protocol.Cipher, "2022") {
		return
	}
	length := 32
	if protocol.Cipher == "2022-blake3-aes-128-gcm" {
		length = 16
	}
	if strings.TrimSpace(protocol.ServerKey) == "" {
		if key := strings.TrimSpace(existing["shadowsocks"]); len(key) == length {
			protocol.ServerKey = key
			return
		}
	}
	if len(protocol.ServerKey) != length {
		protocol.ServerKey = protocolkey.GenerateCipher(protocol.ServerKey, length)
	}
}

func realityProtocolKeyLookup(protocols []node.Protocol) map[string]realityProtocolKey {
	keys := make(map[string]realityProtocolKey, len(protocols))
	for _, protocol := range protocols {
		if strings.ToLower(strings.TrimSpace(protocol.Security)) != "reality" {
			continue
		}
		protocolType := normalizedProtocolType(protocol.Type)
		key := realityProtocolKey{
			privateKey: strings.TrimSpace(protocol.RealityPrivateKey),
			publicKey:  strings.TrimSpace(protocol.RealityPublicKey),
			shortID:    strings.TrimSpace(protocol.RealityShortId),
		}
		if protocolType == "" || !key.complete() {
			continue
		}
		keys[protocolType] = key
	}
	return keys
}

func ensureRealityProtocolKey(protocol *node.Protocol, existing map[string]realityProtocolKey) error {
	if strings.ToLower(strings.TrimSpace(protocol.Security)) != "reality" {
		return nil
	}
	key := realityProtocolKey{
		privateKey: strings.TrimSpace(protocol.RealityPrivateKey),
		publicKey:  strings.TrimSpace(protocol.RealityPublicKey),
		shortID:    strings.TrimSpace(protocol.RealityShortId),
	}
	if key.complete() {
		return nil
	}
	if oldKey, ok := existing[normalizedProtocolType(protocol.Type)]; ok && oldKey.complete() {
		protocol.RealityPrivateKey = oldKey.privateKey
		protocol.RealityPublicKey = oldKey.publicKey
		protocol.RealityShortId = oldKey.shortID
		return nil
	}
	public, private, err := protocolkey.Curve25519Genkey(false, "")
	if err != nil {
		return err
	}
	protocol.RealityPublicKey = public
	protocol.RealityPrivateKey = private
	protocol.RealityShortId = protocolkey.GenerateShortID(private)
	return nil
}

func ensureRealityProtocolDefaults(protocol *node.Protocol) {
	if strings.ToLower(strings.TrimSpace(protocol.Security)) != "reality" {
		return
	}
	if protocol.RealityServerAddr == "" {
		protocol.RealityServerAddr = protocol.SNI
	}
	if protocol.RealityServerPort == 0 {
		protocol.RealityServerPort = 443
	}
}

func (k realityProtocolKey) complete() bool {
	return k.privateKey != "" && k.publicKey != "" && k.shortID != ""
}

// Snell is absent on purpose: its users authenticate with their own UUID, so
// there is no listener key left to generate.
func generatedKeyProtocolType(raw string) string {
	switch normalizedProtocolType(raw) {
	case "ssr", "shadowsocks-r", "shadowsocksr":
		return "shadowsocksr"
	default:
		return ""
	}
}

func normalizedProtocolType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "hysteria", "hysteria2":
		return "hysteria"
	case "ssr", "shadowsocks-r", "shadowsocksr":
		return "shadowsocksr"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}
