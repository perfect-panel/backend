package storefront

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QuerySubscribeList lists the plans on sale in the requested language,
// falling back to the default-language plans when there are none.
func (s *Service) QuerySubscribeList(ctx context.Context, req *dto.QuerySubscribeListRequest) (*dto.QuerySubscribeListResponse, error) {
	total, data, err := s.deps.Plans.FilterList(ctx, &subscribe.FilterParams{
		Page:            1,
		Size:            9999,
		Language:        req.Language,
		Sell:            true,
		DefaultLanguage: true,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[QuerySubscribeListLogic] Database Error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "QuerySubscribeList error: %v", err.Error())
	}

	list := make([]dto.Subscribe, len(data))
	for i, item := range data {
		var sub dto.Subscribe
		if err := mapping.Copy(&sub, item); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map plan %d", item.Id)
		}
		if item.Discount != "" {
			var discount []dto.SubscribeDiscount
			_ = json.Unmarshal([]byte(item.Discount), &discount)
			sub.Discount = discount
		}
		list[i] = sub
	}
	return &dto.QuerySubscribeListResponse{
		Total: total,
		List:  list,
	}, nil
}
