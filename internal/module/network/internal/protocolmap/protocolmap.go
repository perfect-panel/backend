// Package protocolmap is the one mapping between a server protocol as stored
// (node.Protocol, the servers.protocols JSON) and as the API shows it
// (dto.Protocol). The two share their JSON names by contract, so the mapping
// goes through them; the one stored-only field is the certificate pin a node
// reports, which the API neither shows nor sets.
package protocolmap

import (
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ToDTO returns the API form of stored protocols.
func ToDTO(protocols []node.Protocol) ([]dto.Protocol, error) {
	result := make([]dto.Protocol, 0, len(protocols))
	for _, protocol := range protocols {
		var shown dto.Protocol
		if err := convert(protocol, &shown); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map %s protocol", protocol.Type)
		}
		result = append(result, shown)
	}
	return result, nil
}

// FromDTO returns the stored form of an API protocol, without a certificate
// pin: an update merges the stored one back.
func FromDTO(protocol dto.Protocol) (node.Protocol, error) {
	var stored node.Protocol
	if err := convert(protocol, &stored); err != nil {
		return node.Protocol{}, xerr.Wrapf(err, xerr.InvalidParams, "map %s protocol", protocol.Type)
	}
	return stored, nil
}

func convert(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, to)
}
