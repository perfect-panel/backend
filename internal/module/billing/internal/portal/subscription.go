package portal

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSubscription lists the storefront's visible subscription plans.
func (s *Service) GetSubscription(ctx context.Context, req *dto.GetSubscriptionRequest) (*dto.GetSubscriptionResponse, error) {
	_, data, err := s.deps.Plans.FilterList(ctx, &subscribe.FilterParams{
		Page:            1,
		Size:            9999,
		Show:            true,
		Language:        req.Language,
		DefaultLanguage: true,
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get subscription list")
	}
	list := make([]dto.BillingSubscribeSnapshot, len(data))
	for i, item := range data {
		if err := mapping.Copy(&list[i], item); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map subscribe %d", item.Id)
		}
		// The copy leaves the tiers out: they are stored as JSON text.
		if item.Discount != "" {
			var discount []dto.BillingSubscribeDiscount
			_ = json.Unmarshal([]byte(item.Discount), &discount)
			list[i].Discount = discount
		}
	}
	return &dto.GetSubscriptionResponse{List: list}, nil
}
