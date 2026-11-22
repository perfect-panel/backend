package adminuser

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserLoginLogs pages the sign-in audits of an account.
func (s *Service) GetUserLoginLogs(ctx context.Context, req *dto.GetUserLoginLogsRequest) (*dto.GetUserLoginLogsResponse, error) {
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: req.UserId,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetUserLoginLogs] get user login logs failed", logger.Field("error", err.Error()), logger.Field("request", req))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get user login logs failed: %v", err.Error())
	}
	var list []dto.UserLoginLog

	for _, datum := range data {
		var content log.Login
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "corrupt login log %d", datum.Id)
		}
		list = append(list, dto.UserLoginLog{
			Id:               datum.Id,
			UserId:           datum.ObjectID,
			LoginIP:          content.LoginIP,
			UserAgent:        content.UserAgent,
			Success:          content.Success,
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

	return &dto.GetUserLoginLogsResponse{
		Total: total,
		List:  list,
	}, nil
}
