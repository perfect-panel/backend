package profile

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetLoginLog pages the sign-in audits of the calling account.
func (s *Service) GetLoginLog(ctx context.Context, req *dto.GetLoginLogRequest) (*dto.GetLoginLogResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("find login log failed:", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find login log failed: %v", err.Error())
	}
	list := make([]dto.UserLoginLog, 0)

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

	return &dto.GetLoginLogResponse{
		Total: total,
		List:  list,
	}, nil
}
