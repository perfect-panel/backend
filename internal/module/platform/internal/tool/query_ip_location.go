package tool

import (
	"context"
	"net"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryIPLocation looks an IP address up in the local GeoIP database.
func (s *Service) QueryIPLocation(ctx context.Context, req *dto.QueryIPLocationRequest) (*dto.QueryIPLocationResponse, error) {
	ip := net.ParseIP(req.IP)
	if ip == nil {
		return nil, xerr.Errorf(xerr.InvalidParams, "not an IP address")
	}
	// The database may be missing or still downloading: the accessor then
	// returns nil, and a nil reader must not be dereferenced.
	if s.deps.GeoIP == nil {
		return nil, xerr.Errorf(xerr.ERROR, "GeoIP database not configured")
	}
	reader := s.deps.GeoIP()
	if reader == nil {
		return nil, xerr.Errorf(xerr.ERROR, "GeoIP database not loaded")
	}
	record, err := reader.City(ip)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "query IP location: %v", err)
	}

	var country, region, city string
	if record.Country.Names != nil {
		country = record.Country.Names["en"]
	}
	if len(record.Subdivisions) > 0 && record.Subdivisions[0].Names != nil {
		region = record.Subdivisions[0].Names["en"]
	}
	if record.City.Names != nil {
		city = record.City.Names["en"]
	}
	return &dto.QueryIPLocationResponse{Country: country, Region: region, City: city}, nil
}
