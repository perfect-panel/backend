package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/internal/protocolmap"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetServerProtocols returns the protocols a server offers, as the API shows
// them.
func (s *Service) GetServerProtocols(ctx context.Context, req *dto.GetServerProtocolsRequest) (*dto.GetServerProtocolsResponse, error) {
	log := logger.WithContext(ctx)
	data, err := s.deps.Store.Node().FindOneServer(ctx, req.Id)
	if err != nil {
		log.Errorf("[GetServerProtocols] FindOneServer Error: %s", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "[GetServerProtocols] FindOneServer Error: %s", err.Error())
	}

	dst, err := data.UnmarshalProtocols()
	if err != nil {
		log.Errorf("[GetServerProtocols] UnmarshalProtocols Error: %s", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "unmarshal protocols of server %d", req.Id)
	}
	protocols, err := protocolmap.ToDTO(dst)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "map protocols of server %d", req.Id)
	}

	return &dto.GetServerProtocolsResponse{
		Protocols: protocols,
	}, nil
}
