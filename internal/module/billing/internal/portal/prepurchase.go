package portal

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// PrePurchase previews the price of a guest order without creating it. A
// guest holds no gift credit, so the preview prices exactly like Purchase.
func (s *Service) PrePurchase(ctx context.Context, req *dto.PrePurchaseOrderRequest) (*dto.PrePurchaseOrderResponse, error) {
	plan, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscribe %d", req.SubscribeId)
	}
	terms, err := checkout.ResolvePlanTerms(ctx, s.deps.Coupons, s.deps.Payments, plan, req.Quantity, req.Coupon, req.Payment)
	if err != nil {
		return nil, err
	}
	quote := pricing.Compute(terms.Input(0))
	return &dto.PrePurchaseOrderResponse{
		Price:          quote.Price,
		Amount:         quote.Amount,
		Discount:       quote.Discount,
		Coupon:         req.Coupon,
		CouponDiscount: quote.CouponDiscount,
		FeeAmount:      quote.FeeAmount,
	}, nil
}
