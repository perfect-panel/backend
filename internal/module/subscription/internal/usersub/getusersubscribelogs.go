package usersub

import (
	"context"
	"strconv"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserSubscribeLogs pages the user's subscription fetch log, optionally
// only the fetches of one subscription.
func (s *Service) GetUserSubscribeLogs(ctx context.Context, req *dto.GetUserSubscribeLogsRequest) (*dto.GetUserSubscribeLogsResponse, error) {
	params := &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: req.UserId,
	}
	if req.SubscribeId != 0 {
		params.Search = `"user_subscribe_id":` + strconv.FormatInt(req.SubscribeId, 10)
	}

	lg := logger.WithContext(ctx)
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, params)
	if err != nil {
		lg.Errorw("[GetUserSubscribeLogs] Get User Subscribe Logs Error:", logger.Field("err", err.Error()))
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "Get User Subscribe Logs Error")
	}
	var list []dto.UserSubscribeLog

	for _, datum := range data {
		var content log.Subscribe
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			lg.Errorf("[GetUserSubscribeLogs] unmarshal subscribe log content failed: %v", err.Error())
			return nil, xerr.Wrapf(err, xerr.ERROR, "corrupt subscription log %d: %v", datum.Id, err)
		}
		list = append(list, dto.UserSubscribeLog{
			Id:               datum.Id,
			UserId:           datum.ObjectID,
			UserSubscribeId:  content.UserSubscribeId,
			Token:            content.Token,
			IP:               content.ClientIP,
			UserAgent:        content.UserAgent,
			Timestamp:        datum.CreatedAt.UnixMilli(),
			ActorID:          content.ActorID,
			IPCountryCode:    content.IPCountryCode,
			IPCountry:        content.IPCountry,
			IPRegion:         content.IPRegion,
			IPCity:           content.IPCity,
			IPASN:            content.IPASN,
			IPASOrganization: content.IPASOrganization,
		})
	}

	return &dto.GetUserSubscribeLogsResponse{
		List:  list,
		Total: total,
	}, nil
}
