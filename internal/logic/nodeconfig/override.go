package nodeconfig

import (
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/pkg/errors"
)

func GlobalValues(c config.NodeConfig) dto.ServerNodeConfigValues {
	dns := make([]dto.NodeDNS, 0, len(c.DNS))
	for _, d := range c.DNS {
		dns = append(dns, dto.NodeDNS{
			Proto:   d.Proto,
			Address: d.Address,
			Domains: normalizeStrings(d.Domains),
		})
	}

	outbound := make([]dto.NodeOutbound, 0, len(c.Outbound))
	for _, o := range c.Outbound {
		outbound = append(outbound, dto.NodeOutbound{
			Name:                 o.Name,
			Protocol:             o.Protocol,
			Address:              o.Address,
			Port:                 o.Port,
			User:                 o.User,
			Password:             o.Password,
			UUID:                 o.UUID,
			Cipher:               o.Cipher,
			Security:             o.Security,
			SNI:                  o.SNI,
			AllowInsecure:        o.AllowInsecure,
			Fingerprint:          o.Fingerprint,
			Transport:            o.Transport,
			Host:                 o.Host,
			Path:                 o.Path,
			ServiceName:          o.ServiceName,
			Flow:                 o.Flow,
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
		IPStrategy: c.IPStrategy,
		DNS:        ensureDNS(dns),
		Block:      normalizeStrings(c.Block),
		Outbound:   ensureOutbound(outbound),
	}
}

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

func CloneValues(values dto.ServerNodeConfigValues) dto.ServerNodeConfigValues {
	dns := make([]dto.NodeDNS, 0, len(values.DNS))
	for _, d := range values.DNS {
		dns = append(dns, dto.NodeDNS{
			Proto:   d.Proto,
			Address: d.Address,
			Domains: normalizeStrings(d.Domains),
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
			Security:             o.Security,
			SNI:                  o.SNI,
			AllowInsecure:        o.AllowInsecure,
			Fingerprint:          o.Fingerprint,
			Transport:            o.Transport,
			Host:                 o.Host,
			Path:                 o.Path,
			ServiceName:          o.ServiceName,
			Flow:                 o.Flow,
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
		return errors.Wrapf(err, "unmarshal server node config %s", field)
	}
	return nil
}

func marshalJSONField(value any, field string) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", errors.Wrapf(err, "marshal server node config %s", field)
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
			Proto:   proto,
			Address: address,
			Domains: normalizeStrings(item.Domains),
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
			Security:             strings.TrimSpace(item.Security),
			SNI:                  strings.TrimSpace(item.SNI),
			AllowInsecure:        item.AllowInsecure,
			Fingerprint:          strings.TrimSpace(item.Fingerprint),
			Transport:            strings.TrimSpace(item.Transport),
			Host:                 strings.TrimSpace(item.Host),
			Path:                 strings.TrimSpace(item.Path),
			ServiceName:          strings.TrimSpace(item.ServiceName),
			Flow:                 strings.TrimSpace(item.Flow),
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
