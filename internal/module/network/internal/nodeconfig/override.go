// Package nodeconfig resolves the node configuration a server's nodes run
// with: the global node settings and the server's override of them, whose
// fields each either inherit (NULL) or replace a global value.
package nodeconfig

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// GlobalValues is the node configuration every server inherits, normalized
// like any other configuration values.
func GlobalValues(c config.NodeConfig) dto.ServerNodeConfigValues {
	values := dto.ServerNodeConfigValues{IPStrategy: c.IPStrategy, Block: c.Block}
	for _, d := range c.DNS {
		values.DNS = append(values.DNS, dto.NodeDNS(d))
	}
	for _, o := range c.Outbound {
		values.Outbound = append(values.Outbound, dto.NodeOutbound(o))
	}
	return CloneValues(values)
}

// ApplyOverride replaces the values the override sets; a missing override
// changes nothing.
func ApplyOverride(values *dto.ServerNodeConfigValues, override *node.ServerConfigOverride) error {
	if values == nil || override == nil || override.Id == 0 {
		return nil
	}

	if override.IPStrategy != nil {
		values.IPStrategy = *override.IPStrategy
	}
	if override.DNS != nil {
		var dns []dto.NodeDNS
		if err := unmarshalJSONField(*override.DNS, &dns, "dns"); err != nil {
			return err
		}
		values.DNS = ensureDNS(dns)
	}
	if override.Block != nil {
		var block []string
		if err := unmarshalJSONField(*override.Block, &block, "block"); err != nil {
			return err
		}
		values.Block = normalizeStrings(block)
	}
	if override.Outbound != nil {
		var outbound []dto.NodeOutbound
		if err := unmarshalJSONField(*override.Outbound, &outbound, "outbound"); err != nil {
			return err
		}
		values.Outbound = ensureOutbound(outbound)
	}
	return nil
}

// OverrideResponse is the API form of a server's override: each value it
// does not set is marked inherited.
func OverrideResponse(override *node.ServerConfigOverride) (dto.ServerNodeConfigOverride, error) {
	resp := dto.ServerNodeConfigOverride{
		InheritIPStrategy: true,
		InheritDNS:        true,
		InheritBlock:      true,
		InheritOutbound:   true,
		DNS:               []dto.NodeDNS{},
		Block:             []string{},
		Outbound:          []dto.NodeOutbound{},
	}
	if override == nil || override.Id == 0 {
		return resp, nil
	}

	if override.IPStrategy != nil {
		resp.InheritIPStrategy = false
		resp.IPStrategy = *override.IPStrategy
	}
	if override.DNS != nil {
		resp.InheritDNS = false
		var dns []dto.NodeDNS
		if err := unmarshalJSONField(*override.DNS, &dns, "dns"); err != nil {
			return resp, err
		}
		resp.DNS = ensureDNS(dns)
	}
	if override.Block != nil {
		resp.InheritBlock = false
		var block []string
		if err := unmarshalJSONField(*override.Block, &block, "block"); err != nil {
			return resp, err
		}
		resp.Block = normalizeStrings(block)
	}
	if override.Outbound != nil {
		resp.InheritOutbound = false
		var outbound []dto.NodeOutbound
		if err := unmarshalJSONField(*override.Outbound, &outbound, "outbound"); err != nil {
			return resp, err
		}
		resp.Outbound = ensureOutbound(outbound)
	}

	return resp, nil
}

// OverrideModel is the stored form of a server's override as the API sets
// it, and whether it inherits every value, in which case nothing need be
// stored.
func OverrideModel(serverID int64, req dto.ServerNodeConfigOverride) (*node.ServerConfigOverride, bool, error) {
	data := &node.ServerConfigOverride{
		ServerId: serverID,
	}

	if !req.InheritIPStrategy {
		data.IPStrategy = stringPtr(req.IPStrategy)
	}
	if !req.InheritDNS {
		value, err := marshalJSONField(ensureDNS(req.DNS), "dns")
		if err != nil {
			return nil, false, err
		}
		data.DNS = &value
	}
	if !req.InheritBlock {
		value, err := marshalJSONField(normalizeStrings(req.Block), "block")
		if err != nil {
			return nil, false, err
		}
		data.Block = &value
	}
	if !req.InheritOutbound {
		value, err := marshalJSONField(ensureOutbound(req.Outbound), "outbound")
		if err != nil {
			return nil, false, err
		}
		data.Outbound = &value
	}

	allInherited := data.IPStrategy == nil && data.DNS == nil && data.Block == nil && data.Outbound == nil
	return data, allInherited, nil
}

// CloneValues deep-copies values, normalizing its string lists and giving the
// DNS and outbound lists their defaults when empty.
func CloneValues(values dto.ServerNodeConfigValues) dto.ServerNodeConfigValues {
	dns := make([]dto.NodeDNS, 0, len(values.DNS))
	for _, d := range values.DNS {
		dns = append(dns, dto.NodeDNS{
			Proto:      d.Proto,
			Address:    d.Address,
			ServerName: d.ServerName,
			Domains:    normalizeStrings(d.Domains),
		})
	}

	outbound := make([]dto.NodeOutbound, 0, len(values.Outbound))
	for _, o := range values.Outbound {
		outbound = append(outbound, dto.NodeOutbound{
			Name:                 o.Name,
			Protocol:             o.Protocol,
			Address:              o.Address,
			Port:                 o.Port,
			User:                 o.User,
			Password:             o.Password,
			UUID:                 o.UUID,
			Cipher:               o.Cipher,
			Plugin:               o.Plugin,
			PluginOptions:        o.PluginOptions,
			Security:             o.Security,
			SNI:                  o.SNI,
			ALPN:                 normalizeStrings(o.ALPN),
			AllowInsecure:        o.AllowInsecure,
			Fingerprint:          o.Fingerprint,
			Transport:            o.Transport,
			Host:                 o.Host,
			Path:                 o.Path,
			ServiceName:          o.ServiceName,
			XHTTPMode:            o.XHTTPMode,
			XHTTPExtra:           o.XHTTPExtra,
			Flow:                 o.Flow,
			Encryption:           o.Encryption,
			EncryptionMode:       o.EncryptionMode,
			EncryptionRTT:        o.EncryptionRTT,
			EncryptionTicket:     o.EncryptionTicket,
			EncryptionPadding:    o.EncryptionPadding,
			EncryptionPassword:   o.EncryptionPassword,
			Multiplex:            o.Multiplex,
			UoT:                  o.UoT,
			UoTVersion:           o.UoTVersion,
			CongestionController: o.CongestionController,
			UDPStream:            o.UDPStream,
			ReduceRtt:            o.ReduceRtt,
			Heartbeat:            o.Heartbeat,
			RealityPublicKey:     o.RealityPublicKey,
			RealityShortId:       o.RealityShortId,
			SpiderX:              o.SpiderX,
			Settings:             o.Settings,
			StreamSettings:       o.StreamSettings,
			Rules:                normalizeStrings(o.Rules),
		})
	}

	return dto.ServerNodeConfigValues{
		IPStrategy: values.IPStrategy,
		DNS:        ensureDNS(dns),
		Block:      normalizeStrings(values.Block),
		Outbound:   ensureOutbound(outbound),
	}
}

func unmarshalJSONField[T any](value string, target *T, field string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return fmt.Errorf("unmarshal server node config %s: %w", field, err)
	}
	return nil
}

func marshalJSONField(value any, field string) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal server node config %s: %w", field, err)
	}
	return string(data), nil
}

func normalizeStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func ensureDNS(values []dto.NodeDNS) []dto.NodeDNS {
	if values == nil {
		return []dto.NodeDNS{}
	}
	result := make([]dto.NodeDNS, 0, len(values))
	for _, item := range values {
		proto := strings.TrimSpace(item.Proto)
		address := strings.TrimSpace(item.Address)
		if proto == "" || address == "" {
			continue
		}
		result = append(result, dto.NodeDNS{
			Proto:      proto,
			Address:    address,
			ServerName: strings.TrimSpace(item.ServerName),
			Domains:    normalizeStrings(item.Domains),
		})
	}
	return result
}

func ensureOutbound(values []dto.NodeOutbound) []dto.NodeOutbound {
	if values == nil {
		return []dto.NodeOutbound{}
	}
	result := make([]dto.NodeOutbound, 0, len(values))
	for _, item := range values {
		name := strings.TrimSpace(item.Name)
		protocol := strings.TrimSpace(item.Protocol)
		rules := normalizeStrings(item.Rules)
		if name == "" || protocol == "" {
			continue
		}
		result = append(result, dto.NodeOutbound{
			Name:                 name,
			Protocol:             protocol,
			Address:              strings.TrimSpace(item.Address),
			Port:                 item.Port,
			User:                 strings.TrimSpace(item.User),
			Password:             item.Password,
			UUID:                 strings.TrimSpace(item.UUID),
			Cipher:               strings.TrimSpace(item.Cipher),
			Plugin:               strings.TrimSpace(item.Plugin),
			PluginOptions:        item.PluginOptions,
			Security:             strings.TrimSpace(item.Security),
			SNI:                  strings.TrimSpace(item.SNI),
			ALPN:                 normalizeStrings(item.ALPN),
			AllowInsecure:        item.AllowInsecure,
			Fingerprint:          strings.TrimSpace(item.Fingerprint),
			Transport:            strings.TrimSpace(item.Transport),
			Host:                 strings.TrimSpace(item.Host),
			Path:                 strings.TrimSpace(item.Path),
			ServiceName:          strings.TrimSpace(item.ServiceName),
			XHTTPMode:            strings.TrimSpace(item.XHTTPMode),
			XHTTPExtra:           strings.TrimSpace(item.XHTTPExtra),
			Flow:                 strings.TrimSpace(item.Flow),
			Encryption:           strings.TrimSpace(item.Encryption),
			EncryptionMode:       strings.TrimSpace(item.EncryptionMode),
			EncryptionRTT:        strings.TrimSpace(item.EncryptionRTT),
			EncryptionTicket:     strings.TrimSpace(item.EncryptionTicket),
			EncryptionPadding:    strings.TrimSpace(item.EncryptionPadding),
			EncryptionPassword:   item.EncryptionPassword,
			Multiplex:            strings.TrimSpace(item.Multiplex),
			UoT:                  item.UoT,
			UoTVersion:           item.UoTVersion,
			CongestionController: strings.TrimSpace(item.CongestionController),
			UDPStream:            item.UDPStream,
			ReduceRtt:            item.ReduceRtt,
			Heartbeat:            item.Heartbeat,
			RealityPublicKey:     strings.TrimSpace(item.RealityPublicKey),
			RealityShortId:       strings.TrimSpace(item.RealityShortId),
			SpiderX:              strings.TrimSpace(item.SpiderX),
			Settings:             strings.TrimSpace(item.Settings),
			StreamSettings:       strings.TrimSpace(item.StreamSettings),
			Rules:                rules,
		})
	}
	return result
}

func stringPtr(value string) *string {
	return &value
}
