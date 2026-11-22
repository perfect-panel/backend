package usersub

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserSubscribe lists every subscription of the user, whatever its
// status, with its plan.
func (s *Service) GetUserSubscribe(ctx context.Context, req *dto.GetUserSubscribeListRequest) (*dto.GetUserSubscribeListResponse, error) {
	data, err := s.deps.UserSubs.QueryUserSubscribe(ctx, req.UserId, usersub.AllStatuses.Values()...)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetUserSubscribeLogs] Get User Subscribe Error:", logger.Field("err", err.Error()))
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "Get User Subscribe Error")
	}

	resp := &dto.GetUserSubscribeListResponse{
		List:  make([]dto.UserSubscribe, 0),
		Total: int64(len(data)),
	}

	for _, item := range data {
		var sub dto.UserSubscribe
		if err := mapping.Copy(&sub, item); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map subscription %d", item.Id)
		}
		sub.Short, _ = protocolkey.FixedUniqueString(item.Token, 8, "")
		resp.List = append(resp.List, sub)
	}
	return resp, nil
}
