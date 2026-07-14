package tool

import (
	"context"
	"net"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryIPLocationLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryIPLocationLogic Query IP Location
func NewQueryIPLocationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryIPLocationLogic {
	return &QueryIPLocationLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryIPLocationLogic) QueryIPLocation(req *dto.QueryIPLocationRequest) (resp *dto.QueryIPLocationResponse, err error) {
	if l.svcCtx.GeoIP == nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), " GeoIP database not configured")
	}

	ip := net.ParseIP(req.IP)
	record, err := l.svcCtx.GeoIP.DB.City(ip)
	if err != nil {
		l.Errorf("Failed to query IP location: %v", err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to query IP location")
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

	return &dto.QueryIPLocationResponse{
		Country: country,
		Region:  region,
		City:    city,
	}, nil
}
