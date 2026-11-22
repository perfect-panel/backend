package checkout

import (
	"context"
	"errors"
	"fmt"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// closeActorKind identifies who asks for a pending order to be closed.
type closeActorKind uint8

const (
	// closeBySystem is the expiry task or the pending-order reconciler; it
	// closes only what the gateway confirms is safe to close.
	closeBySystem closeActorKind = iota
	// closeByOwner is the order's owner abandoning the order.
	closeByOwner
	// closeByAdmin is an administrator resolving the order by hand.
	closeByAdmin
)

type closeActor struct {
	kind   closeActorKind
	userID int64 // the owner's or the administrator's user id
}

// explicit reports whether a person deliberately gave the order up, which
// forfeits an EPay/Alipay payment the gateway cannot confirm.
func (a closeActor) explicit() bool { return a.kind != closeBySystem }

// Close closes a pending order: the billing transaction releases the coupon
// reservation and refunds the gift deduction, then the reserved plan
// inventory returns in its own subscription-domain transaction (ADR-001
// step 2). Orders whose gateway checkout already collected money are settled
// instead of closed.
func (s *Service) Close(ctx context.Context, req *dto.CloseOrderRequest) error {
	// Public callers are authenticated by the route. Queue workers use a
	// context without a user and are the only internal callers allowed to close
	// any expired order.
	actor := closeActor{kind: closeBySystem}
	if currentUser, ok := user.FromContext(ctx); ok {
		actor = closeActor{kind: closeByOwner, userID: currentUser.Id}
	}
	_, err := s.closeOrder(ctx, req.OrderNo, actor)
	return err
}

// CloseByAdmin closes a pending order for an administrator through the same
// flow as owner and expiry closes, so the coupon, gift deduction and plan
// inventory are released and a cancellable gateway payment is voided first;
// a payment the gateway confirms is settled instead. It reports whether this
// call closed the order.
func (s *Service) CloseByAdmin(ctx context.Context, orderNo string, adminID int64) (bool, error) {
	return s.closeOrder(ctx, orderNo, closeActor{kind: closeByAdmin, userID: adminID})
}

func (s *Service) closeOrder(ctx context.Context, orderNo string, actor closeActor) (bool, error) {
	closed, err := s.closeOrderOnce(ctx, orderNo, actor)
	if errors.Is(err, ErrGatewayUnconfirmed) {
		// The order stays pending until the gateway confirms the payment:
		// a retryable business conflict, not a server failure.
		return false, xerr.Wrapf(err, xerr.PaymentStatusUnconfirmed, "close order %s", orderNo)
	}
	return closed, err
}

func (s *Service) closeOrderOnce(ctx context.Context, orderNo string, actor closeActor) (bool, error) {
	log := logger.WithContext(ctx)
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Nothing to close; a repeated close of a removed order succeeds.
		log.Infow("[CloseOrder] Order not found", logger.Field("orderNo", orderNo))
		return false, nil
	}
	if err != nil {
		return false, xerr.Wrapf(err, xerr.DatabaseQueryError, "find order %s", orderNo)
	}
	if actor.kind == closeByOwner && orderInfo.UserId != actor.userID {
		return false, xerr.Errorf(xerr.InvalidAccess, "order does not belong to the current user")
	}
	if !order.CanClose(orderInfo.Status) {
		log.Infow("[CloseOrder] Order is no longer pending",
			logger.Field("orderNo", orderNo),
			logger.Field("status", orderInfo.Status),
		)
		if orderInfo.Status == order.StatusClosed {
			// Resume a restoration lost between the close commit and the
			// inventory transaction; RestoreInventoryOnce no-ops when the
			// order never reserved or already restored.
			return false, s.restoreReservedInventory(ctx, orderInfo)
		}
		return false, nil
	}
	verdict, err := s.reconcileGateway(ctx, orderInfo, actor.explicit())
	if err != nil {
		return false, err
	}
	if verdict.TradeNo != "" {
		// The gateway collected the payment: settle instead of closing.
		return false, s.settleVerifiedPayment(ctx, orderInfo, verdict.TradeNo)
	}
	closed, err := s.closePending(ctx, orderInfo, verdict.RequireStableCheckout)
	if err != nil || !closed {
		return false, err
	}
	if actor.kind == closeByAdmin {
		log.Infow("[CloseOrder] Administrator closed pending order",
			logger.Field("orderNo", orderNo),
			logger.Field("adminId", actor.userID),
		)
	}
	// The reserved plan inventory returns in its own subscription-domain
	// transaction (ADR-001 step 2). A crash before this point is resumed by
	// the retried close task via the closed-order branch above.
	return true, s.restoreReservedInventory(ctx, orderInfo)
}

// reconcileGateway asks the order's gateway whether the pending order may
// close, so closing locally cannot leave an active provider checkout able to
// charge the buyer after stock and coupons have been released.
func (s *Service) reconcileGateway(ctx context.Context, orderInfo *order.Order, explicit bool) (gateway.Reconciliation, error) {
	if !s.deps.Gateways.Handles(orderInfo.Method) {
		return gateway.Reconciliation{}, nil
	}
	if orderInfo.PaymentCurrency == "" && orderInfo.TradeNo == "" {
		// Checkout never started, so no gateway holds a payment.
		return s.deps.Gateways.CloseWithoutCheckout(orderInfo.Method), nil
	}
	// Reconciliation must work for a method an administrator has disabled
	// since, so the method is loaded without the availability check.
	method, err := s.deps.Payments.FindOne(ctx, orderInfo.PaymentId)
	if err != nil {
		return gateway.Reconciliation{}, xerr.Wrapf(err, xerr.DatabaseQueryError, "find payment method %d", orderInfo.PaymentId)
	}
	gw, err := s.deps.Gateways.Open(method)
	if err != nil {
		return gateway.Reconciliation{}, xerr.Wrapf(err, xerr.ERROR, "open payment gateway of order %s", orderInfo.OrderNo)
	}
	return gw.Reconcile(ctx, gateway.CloseRequest{
		Order: orderInfo, Explicit: explicit, SystemCurrency: s.systemCurrency(),
	})
}

func (s *Service) systemCurrency() string {
	if s.deps.CurrencyUnit == nil {
		return ""
	}
	return s.deps.CurrencyUnit()
}

// closePending closes the pending order and returns what it held: the coupon
// reservation and the gift credit. Only the still-pending order may close: a
// payment callback can race this close, and an unconditional status write
// would turn a paid order back into a closed one.
func (s *Service) closePending(ctx context.Context, snapshot *order.Order, requireStableCheckout bool) (bool, error) {
	var closed bool
	err := s.deps.Tx.InBillingTx(ctx, func(tx repository.BillingStore) error {
		if requireStableCheckout {
			// Serialize with the checkout's expectation write so a verdict on
			// one checkout cannot close another that started meanwhile.
			current, err := tx.Order().FindOneByOrderNoForUpdate(ctx, snapshot.OrderNo)
			if err != nil {
				return xerr.Wrapf(err, xerr.DatabaseQueryError, "lock order %s", snapshot.OrderNo)
			}
			if !order.CanClose(current.Status) {
				return nil
			}
			if checkoutChanged(current, snapshot) {
				return fmt.Errorf("checkout of order %s changed during cancellation: %w", snapshot.OrderNo, ErrGatewayUnconfirmed)
			}
		}
		var err error
		closed, err = tx.Order().UpdateOrderStatusFrom(ctx, snapshot.OrderNo, order.StatusPending, order.StatusClosed)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "close order %s", snapshot.OrderNo)
		}
		if !closed {
			return nil
		}
		if snapshot.Coupon != "" && snapshot.CouponReserved {
			if err := tx.Coupon().ReleaseUsage(ctx, snapshot.Coupon); err != nil {
				return xerr.Wrapf(err, xerr.DatabaseUpdateError, "release coupon %q", snapshot.Coupon)
			}
		}
		// Closed guest orders are kept for payment audit and reconciliation:
		// deleting them would discard evidence of a late provider payment.
		return refundGift(ctx, tx, snapshot)
	})
	if err != nil {
		return false, err
	}
	return closed, nil
}

// checkoutChanged reports whether the order's checkout differs from the
// snapshot the gateway verdict was made on.
func checkoutChanged(current, snapshot *order.Order) bool {
	return current.PaymentCurrency != snapshot.PaymentCurrency || current.PaymentAmount != snapshot.PaymentAmount ||
		current.TradeNo != snapshot.TradeNo || current.PaymentId != snapshot.PaymentId || current.Method != snapshot.Method ||
		current.PaymentSession != snapshot.PaymentSession
}

// refundGift returns the gift credit the order reserved to its buyer.
func refundGift(ctx context.Context, tx repository.BillingStore, o *order.Order) error {
	if o.GiftAmount <= 0 {
		return nil
	}
	wallet, err := lockWallet(ctx, tx, o.UserId)
	if err != nil {
		return err
	}
	wallet.GiftAmount += o.GiftAmount
	if err := tx.Wallet().UpdateBalanceFields(ctx, wallet); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "refund gift credit of order %s", o.OrderNo)
	}
	return ledger.RefundGift(ctx, tx.Log(), ledger.Gift{
		UserID: wallet.UserId, OrderNo: o.OrderNo, Amount: o.GiftAmount, Balance: wallet.GiftAmount,
		Remark: ledger.RemarkCancellationRefund,
	})
}

// restoreReservedInventory returns the closed order's reserved inventory
// unit. Only new subscription purchases reserve plan inventory; renewals and
// traffic resets reference a plan too, but never consumed stock, and the
// reserve marker check inside RestoreInventoryOnce keeps them (and stock-out
// compensation closes) from adding stock that was never taken.
func (s *Service) restoreReservedInventory(ctx context.Context, orderInfo *order.Order) error {
	if orderInfo.Type != order.TypeSubscribe || orderInfo.SubscribeId <= 0 {
		return nil
	}
	if err := s.deps.Inventory.Restore(ctx, orderInfo.OrderNo, orderInfo.SubscribeId); err != nil {
		logger.WithContext(ctx).Errorw("[CloseOrder] Restore subscribe inventory failed",
			logger.Field("error", err.Error()),
			logger.Field("subscribeId", orderInfo.SubscribeId),
			logger.Field("orderNo", orderInfo.OrderNo),
		)
		return xerr.Wrapf(err, xerr.ERROR, "restore inventory of order %s", orderInfo.OrderNo)
	}
	return nil
}
