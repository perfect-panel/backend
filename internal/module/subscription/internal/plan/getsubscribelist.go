package plan

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSubscribeList pages the plans for the admin list, each with its live
// subscriptions (usersub.LiveStatuses) counted as sold.
func (s *Service) GetSubscribeList(ctx context.Context, req *dto.GetSubscribeListRequest) (*dto.GetSubscribeListResponse, error) {
	log := logger.WithContext(ctx)
	total, list, err := s.deps.Plans.FilterList(ctx, &subscribe.FilterParams{
		Page:     int(req.Page),
		Size:     int(req.Size),
		Language: req.Language,
		Search:   req.Search,
	})
	if err != nil {
		log.Error("[GetSubscribeListLogic] get subscribe list failed: ", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get subscribe list failed: %v", err.Error())
	}
	var (
		subscribeIdList = make([]int64, 0, len(list))
		resultList      = make([]dto.SubscribeItem, 0, len(list))
	)
	for _, item := range list {
		subscribeIdList = append(subscribeIdList, item.Id)
		var sub dto.SubscribeItem
		if err := mapping.Copy(&sub, item); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map plan %d", item.Id)
		}
		if item.Discount != "" {
			if err := json.Unmarshal([]byte(item.Discount), &sub.Discount); err != nil {
				log.Error("[GetSubscribeListLogic] JSON unmarshal failed: ", logger.Field("error", err.Error()), logger.Field("discount", item.Discount))
			}
		}
		nodes, tags, parseErr := item.NodeScope()
		if parseErr != nil {
			return nil, xerr.Wrapf(parseErr, xerr.ERROR, "plan %d nodes: %v", item.Id, parseErr)
		}
		sub.Nodes = dto.StringInt64Slice(nodes)
		sub.NodeTags = tags
		resultList = append(resultList, sub)
	}

	subscribeMaps, err := s.deps.UserSubs.QueryActiveSubscriptions(ctx, subscribeIdList...)
	if err != nil {
		log.Error("[GetSubscribeListLogic] get user subscribe failed: ", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get user subscribe failed: %v", err.Error())
	}

	for i, item := range resultList {
		if sub, ok := subscribeMaps[item.Id]; ok {
			resultList[i].Sold = sub
		}
	}

	return &dto.GetSubscribeListResponse{
		Total: total,
		List:  resultList,
	}, nil
}
