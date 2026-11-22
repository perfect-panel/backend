package checkout

import (
	"context"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// PreCreateOrder previews the price of a purchase, or of a renewal when the
// request names the subscription to renew, without creating an order. It
// prices with the same terms and the same Compute as the order it previews,
// using the gift credit the buyer's wallet holds now.
func (s *Service) PreCreateOrder(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PreOrderResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	plan, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscribe %d", req.SubscribeId)
	}
	if req.UserSubscribeId < 0 {
		return nil, xerr.Errorf(xerr.InvalidParams, "invalid user subscribe id")
	}
	if req.UserSubscribeId > 0 {
		if err := s.ensureRenewable(ctx, u.Id, req.UserSubscribeId, req.SubscribeId); err != nil {
			return nil, err
		}
	} else if err := s.ensureQuota(ctx, u.Id, plan); err != nil {
		return nil, err
	}
	terms, err := ResolvePlanTerms(ctx, s.deps.Coupons, s.deps.Payments, plan, quantity, req.Coupon, req.Payment)
	if err != nil {
		return nil, err
	}
	if err := ensureCouponUserLimit(ctx, s.deps.Orders, u.Id, terms.Coupon); err != nil {
		return nil, err
	}
	quote := pricing.Compute(terms.Input(s.giftCredit(ctx, u.Id)))
	return &dto.PreOrderResponse{
		Price:          quote.Price,
		Amount:         quote.Amount,
		Discount:       quote.Discount,
		GiftAmount:     quote.GiftAmount,
		Coupon:         req.Coupon,
		CouponDiscount: quote.CouponDiscount,
		FeeAmount:      quote.FeeAmount,
	}, nil
}

// ensureRenewable checks the subscription a renewal preview names under the
// rules Renewal applies: it must be the buyer's, of the previewed plan,
// managed locally rather than by a payment provider, and neither refunded
// nor stopped.
func (s *Service) ensureRenewable(ctx context.Context, userID, userSubscribeID, planID int64) error {
	userSubscribe, err := s.deps.UserSubs.FindOneSubscribe(ctx, userSubscribeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Errorf(xerr.InvalidParams, "user subscribe not found")
	}
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find user subscribe %d", userSubscribeID)
	}
	if userSubscribe.UserId != userID {
		return xerr.Errorf(xerr.InvalidAccess, "user subscribe does not belong to current user")
	}
	if userSubscribe.SubscribeId != planID {
		return xerr.Errorf(xerr.InvalidParams, "user subscribe does not match subscribe plan")
	}
	if userSubscribe.EntitlementSource != "" {
		return usersub.ErrProviderManaged
	}
	if usersub.OnHold(userSubscribe.Status) {
		return xerr.Errorf(xerr.SubscribeNotAvailable, "refunded or stopped subscription cannot be renewed")
	}
	return nil
}

// giftCredit reads the buyer's gift balance from the authoritative wallet
// row: the context user is the middleware's cached identity snapshot. A
// failed read previews without gift credit.
func (s *Service) giftCredit(ctx context.Context, userID int64) int64 {
	if s.deps.Wallets == nil {
		return 0
	}
	w, err := s.deps.Wallets.FindWallet(ctx, userID)
	if err != nil || w == nil {
		return 0
	}
	return w.GiftAmount
}
