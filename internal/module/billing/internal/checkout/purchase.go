package checkout

import (
	"context"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderaudit"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ordercontext"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/subscription"
	subscribeEntity "github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Purchase creates a pending order for a new subscription. The billing
// transaction locks the buyer's wallet, prices the order with the gift
// credit it holds, reserves the coupon use and the gift credit and inserts
// the order; plan inventory is reserved afterwards in its own
// subscription-domain transaction (ADR-001 step 2).
func (s *Service) Purchase(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PurchaseOrderResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	quantity, err := orderQuantity(req.Quantity)
	if err != nil {
		return nil, err
	}
	if s.deps.SingleModel() {
		blocking, err := s.deps.UserSubs.HasBlockingSubscription(ctx, u.Id)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "check subscriptions of user %d", u.Id)
		}
		if blocking {
			return nil, xerr.Errorf(xerr.UserSubscribeExist, "user has subscription")
		}
	}
	plan, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscribe %d", req.SubscribeId)
	}
	if err := planOnSale(plan); err != nil {
		return nil, err
	}
	if plan.Inventory == 0 {
		return nil, xerr.Errorf(xerr.SubscribeOutOfStock, "subscribe out of stock")
	}
	if err := s.ensureQuota(ctx, u.Id, plan); err != nil {
		return nil, err
	}
	terms, err := ResolvePlanTerms(ctx, s.deps.Coupons, s.deps.Payments, plan, quantity, req.Coupon, req.Payment)
	if err != nil {
		return nil, err
	}
	if terms.Method == nil {
		// A purchase is always paid with a method; zero names none.
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "payment method is required")
	}
	if err := ensureCouponUserLimit(ctx, s.deps.Orders, u.Id, terms.Coupon); err != nil {
		return nil, err
	}
	if err := orderAmountWithinLimit(pricing.Compute(terms.Input(0)).Subtotal()); err != nil {
		return nil, err
	}
	isNew, err := s.deps.Orders.IsUserEligibleForNewOrder(ctx, u.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find orders of user %d", u.Id)
	}
	orderInfo := &order.Order{
		UserId:      u.Id,
		OrderNo:     order.GenerateTradeNo(),
		Type:        order.TypeSubscribe,
		Quantity:    quantity,
		Coupon:      req.Coupon,
		PaymentId:   terms.Method.Id,
		Method:      terms.Method.Platform,
		Status:      order.StatusPending,
		IsNew:       isNew,
		SubscribeId: req.SubscribeId,
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
		// The wallet row lock serializes concurrent purchases by the same
		// user; it does NOT serialize against the activation worker
		// fulfilling an earlier paid order, so a concurrently fulfilled
		// subscription can be missed here. The authoritative gate is
		// fulfillment's own re-check under its user-row lock, which rejects
		// over-quota activation.
		if err := s.ensureQuota(ctx, u.Id, plan); err != nil {
			return err
		}
		ApplyQuote(orderInfo, pricing.Compute(terms.Input(wallet.GiftAmount)))
		if err := orderAmountWithinLimit(orderInfo.Amount); err != nil {
			return err
		}
		if err := ReserveCoupon(ctx, tx, orderInfo); err != nil {
			return err
		}
		if err := spendGift(ctx, tx, wallet, orderInfo, ledger.RemarkPurchaseDeduction); err != nil {
			return err
		}
		return InsertOrder(ctx, tx, orderInfo, orderaudit.SourceUser)
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseInsertError, "create purchase order")
	}
	// On an inventory failure the just-created order is closed, which
	// releases the coupon reservation and refunds the gift credit; the
	// restore step no-ops because nothing was reserved.
	if err := s.deps.Inventory.Reserve(ctx, orderInfo.OrderNo, plan.Id); err != nil {
		if closeErr := s.Close(ctx, &dto.CloseOrderRequest{OrderNo: orderInfo.OrderNo}); closeErr != nil {
			logger.WithContext(ctx).Errorw("[Purchase] Close order after reservation failure failed", logger.Field("error", closeErr.Error()), logger.Field("orderNo", orderInfo.OrderNo))
		}
		if errors.Is(err, subscription.ErrOutOfStock) {
			return nil, xerr.Errorf(xerr.SubscribeOutOfStock, "subscribe out of stock")
		}
		return nil, xerr.Wrapf(err, xerr.ERROR, "reserve inventory")
	}
	s.enqueueDeferredClose(ctx, "[Purchase]", orderInfo.OrderNo)
	return &dto.PurchaseOrderResponse{OrderNo: orderInfo.OrderNo}, nil
}

// ensureQuota rejects a purchase beyond the plan's per-user quota.
func (s *Service) ensureQuota(ctx context.Context, userID int64, plan *subscribeEntity.Subscribe) error {
	if plan.Quota <= 0 {
		return nil
	}
	count, err := s.deps.UserSubs.CountQuotaConsumingSubscriptions(ctx, userID, plan.Id)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "count subscriptions of user %d", userID)
	}
	if count >= plan.Quota {
		return xerr.Errorf(xerr.SubscribeQuotaLimit, "quota limit")
	}
	return nil
}
