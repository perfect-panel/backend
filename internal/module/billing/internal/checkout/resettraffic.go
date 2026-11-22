package checkout

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderaudit"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ordercontext"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ResetTraffic creates a paid traffic-reset order for an active subscription.
// The plan's replacement price is paid like any other order: gift credit
// first, then the payment method's fee on what remains.
func (s *Service) ResetTraffic(ctx context.Context, req *dto.ResetTrafficOrderRequest) (*dto.ResetTrafficOrderResponse, error) {
	u, err := currentUser(ctx)
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
	// A reset restores an exhausted subscription, never a refunded or stopped one.
	if usersub.OnHold(userSubscribe.Status) {
		return nil, xerr.Errorf(xerr.SubscribeNotAvailable, "refunded or stopped subscription cannot reset traffic")
	}
	// A paid traffic reset must not be created for a subscription whose
	// finite term has already elapsed, because it cannot restore access or
	// extend that term. Whether a subscription has a term at all is the
	// subscription entity's rule (usersub.NoExpiry): its no-limit marker may
	// read back shifted by the zone of the process that wrote it.
	if now := timeutil.Now(); !usersub.NoExpiry(userSubscribe.ExpireTime) && !userSubscribe.ExpireTime.After(now) {
		return nil, xerr.Errorf(xerr.SubscribeNotAvailable, "subscription expired")
	}
	if userSubscribe.Subscribe == nil {
		return nil, xerr.Errorf(xerr.ERROR, "subscribe of user subscribe %d not found", req.UserSubscribeID)
	}
	method, err := gateway.LookupMethod(ctx, s.deps.Payments, req.Payment)
	if err != nil {
		return nil, err
	}
	input := pricing.Input{UnitPrice: userSubscribe.Subscribe.Replacement, Quantity: 1, Fee: pricing.FeeTerms(method)}
	orderInfo := &order.Order{
		ParentId:    userSubscribe.OrderId,
		UserId:      u.Id,
		OrderNo:     order.GenerateTradeNo(),
		Type:        order.TypeResetTraffic,
		PaymentId:   method.Id,
		Method:      method.Platform,
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
		input.GiftCredit = wallet.GiftAmount
		ApplyQuote(orderInfo, pricing.Compute(input))
		if err := orderAmountWithinLimit(orderInfo.Amount); err != nil {
			return err
		}
		if err := spendGift(ctx, tx, wallet, orderInfo, ledger.RemarkResetTrafficDeduction); err != nil {
			return err
		}
		return InsertOrder(ctx, tx, orderInfo, orderaudit.SourceUser)
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseInsertError, "create reset traffic order")
	}
	s.enqueueDeferredClose(ctx, "[ResetTraffic]", orderInfo.OrderNo)
	return &dto.ResetTrafficOrderResponse{OrderNo: orderInfo.OrderNo}, nil
}
