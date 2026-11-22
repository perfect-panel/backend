package selfsub

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSubscribeLog pages the owner's subscription fetch log.
func (s *Service) GetSubscribeLog(ctx context.Context, req *dto.GetSubscribeLogRequest) (*dto.GetSubscribeLogResponse, error) {
	lg := logger.WithContext(ctx)
	u, ok := ctx.Value(requestctx.CtxKeyUser).(*user.User)
	if !ok {
		lg.Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		lg.Errorw("[GetUserSubscribeLogs] Get User Subscribe Logs Error:", logger.Field("err", err.Error()))
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "Get User Subscribe Logs Error")
	}
	var list []dto.UserSubscribeLog

	for _, item := range data {
		var content log.Subscribe
		if err = content.Unmarshal([]byte(item.Content)); err != nil {
			lg.Errorf("[GetUserSubscribeLogs] unmarshal subscribe log content failed: %v", err.Error())
			return nil, xerr.Wrapf(err, xerr.ERROR, "corrupt subscription log %d: %v", item.Id, err)
		}
		list = append(list, dto.UserSubscribeLog{
			Id:               item.Id,
			UserId:           item.ObjectID,
			UserSubscribeId:  content.UserSubscribeId,
			Token:            content.Token,
			IP:               content.ClientIP,
			UserAgent:        content.UserAgent,
			Timestamp:        item.CreatedAt.UnixMilli(),
			ActorID:          content.ActorID,
			IPCountryCode:    content.IPCountryCode,
			IPCountry:        content.IPCountry,
			IPRegion:         content.IPRegion,
			IPCity:           content.IPCity,
			IPASN:            content.IPASN,
			IPASOrganization: content.IPASOrganization,
		})
	}

	return &dto.GetSubscribeLogResponse{
		List:  list,
		Total: total,
	}, nil
}
