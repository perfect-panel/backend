package checkout

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderaudit"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ordercontext"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Renewal creates a pending order that extends one of the buyer's
// subscriptions. It is priced like a purchase of the subscription's plan.
func (s *Service) Renewal(ctx context.Context, req *dto.RenewalOrderRequest) (*dto.RenewalOrderResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	quantity, err := orderQuantity(req.Quantity)
	if err != nil {
		return nil, err
	}
	userSubscribe, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, req.UserSubscribeID)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user subscribe %d", req.UserSubscribeID)
	}
	if userSubscribe.UserId != u.Id {
		return nil, xerr.Errorf(xerr.InvalidAccess, "subscription does not belong to the current user")
	}
	if userSubscribe.EntitlementSource != "" {
		return nil, usersub.ErrProviderManaged
	}
	if usersub.OnHold(userSubscribe.Status) {
		return nil, xerr.Errorf(xerr.SubscribeNotAvailable, "refunded or stopped subscription cannot be renewed")
	}
	plan, err := s.deps.Plans.FindOne(ctx, userSubscribe.SubscribeId)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscribe %d", userSubscribe.SubscribeId)
	}
	if err := planOnSale(plan); err != nil {
		return nil, err
	}
	terms, err := ResolvePlanTerms(ctx, s.deps.Coupons, s.deps.Payments, plan, quantity, req.Coupon, req.Payment)
	if err != nil {
		return nil, err
	}
	if terms.Method == nil {
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "payment method is required")
	}
	if err := ensureCouponUserLimit(ctx, s.deps.Orders, u.Id, terms.Coupon); err != nil {
		return nil, err
	}
	if err := orderAmountWithinLimit(pricing.Compute(terms.Input(0)).Subtotal()); err != nil {
		return nil, err
	}
	orderInfo := &order.Order{
		UserId:      u.Id,
		ParentId:    userSubscribe.OrderId,
		OrderNo:     order.GenerateTradeNo(),
		Type:        order.TypeRenewal,
		Quantity:    quantity,
		Coupon:      req.Coupon,
		PaymentId:   terms.Method.Id,
		Method:      terms.Method.Platform,
		Status:      order.StatusPending,
		SubscribeId: userSubscribe.SubscribeId,
		// The subscription is referenced by its id, which survives a token
		// rotation; the token stays for the fulfillment of older releases.
		UserSubscribeId: userSubscribe.Id,
		SubscribeToken:  userSubscribe.Token,
	}
	ordercontext.ApplyIdempotency(ctx, orderInfo)
	err = s.deps.Tx.InBillingTx(ctx, func(tx repository.BillingStore) error {
		wallet, err := lockWallet(ctx, tx, u.Id)
		if err != nil {
			return err
		}
		if err := ensureCouponUserLimit(ctx, tx.Order(), u.Id, terms.Coupon); err != nil {
			return err
		}
		ApplyQuote(orderInfo, pricing.Compute(terms.Input(wallet.GiftAmount)))
		if err := orderAmountWithinLimit(orderInfo.Amount); err != nil {
			return err
		}
		if err := ReserveCoupon(ctx, tx, orderInfo); err != nil {
			return err
		}
		if err := spendGift(ctx, tx, wallet, orderInfo, ledger.RemarkRenewalDeduction); err != nil {
			return err
		}
		return InsertOrder(ctx, tx, orderInfo, orderaudit.SourceUser)
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseInsertError, "create renewal order")
	}
	s.enqueueDeferredClose(ctx, "[Renewal]", orderInfo.OrderNo)
	return &dto.RenewalOrderResponse{OrderNo: orderInfo.OrderNo}, nil
}
