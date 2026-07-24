package portal

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// GetSubscription lists the storefront's visible subscription plans.
func (s *Service) GetSubscription(ctx context.Context, req *dto.GetSubscriptionRequest) (*dto.GetSubscriptionResponse, error) {
	resp := &dto.GetSubscriptionResponse{
		List: make([]dto.Subscribe, 0),
	}
	// Get the subscription list
	_, data, err := s.deps.Plans.FilterList(ctx, &subscribe.FilterParams{
		Page:            1,
		Size:            9999,
		Show:            true,
		Language:        req.Language,
		DefaultLanguage: true,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[Site GetSubscription]", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscription list error: %v", err.Error())
	}
	list := make([]dto.Subscribe, len(data))
	for i, item := range data {
		var sub dto.Subscribe
		tool.DeepCopy(&sub, item)
		if item.Discount != "" {
			var discount []dto.SubscribeDiscount
			_ = json.Unmarshal([]byte(item.Discount), &discount)
			sub.Discount = discount
			list[i] = sub
		}
		list[i] = sub
	}
	resp.List = list
	return resp, nil
}
