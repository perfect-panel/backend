package portal

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/billing/internal/exchangerate"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Checkout starts the payment of a pending order: a gateway checkout the
// buyer completes at the gateway, or an immediate wallet debit.
func (s *Service) Checkout(ctx context.Context, req *dto.CheckoutOrderRequest) (*dto.CheckoutOrderResponse, error) {
	orderInfo, err := s.findOrder(ctx, req.OrderNo)
	if err != nil {
		return nil, err
	}
	if !order.CanCheckout(orderInfo.Status) {
		return nil, xerr.Errorf(xerr.OrderStatusError, "order status error: %v", orderInfo.Status)
	}
	if err := s.authorizeCheckout(ctx, orderInfo, req.CheckoutToken); err != nil {
		return nil, err
	}
	method, err := gateway.LookupMethod(ctx, s.deps.Payments, orderInfo.PaymentId)
	if err != nil {
		return nil, err
	}
	if method.Platform != orderInfo.Method {
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "order %s is bound to %s, not %s", orderInfo.OrderNo, orderInfo.Method, method.Platform)
	}
	if gateway.IsBalance(method) {
		return s.payWithBalance(ctx, orderInfo)
	}
	return s.payThroughGateway(ctx, orderInfo, method, req.ReturnUrl)
}

// authorizeCheckout keeps user-owned orders bound to their authenticated
// owner while guest orders use a short-lived, cryptographically-random
// checkout capability kept only in the temporary-order record.
func (s *Service) authorizeCheckout(ctx context.Context, orderInfo *order.Order, checkoutToken string) error {
	if orderInfo.UserId != 0 {
		currentUser, ok := user.FromContext(ctx)
		if !ok || currentUser.Id != orderInfo.UserId {
			return xerr.Errorf(xerr.InvalidAccess, "order does not belong to the current user")
		}
		return nil
	}
	return s.authorizeGuest(ctx, orderInfo, checkoutToken)
}

// authorizeGuest checks the capability issued when a guest order was
// created.
func (s *Service) authorizeGuest(ctx context.Context, orderInfo *order.Order, checkoutToken string) error {
	if checkoutToken == "" {
		return xerr.Errorf(xerr.InvalidAccess, "guest checkout token is required")
	}
	if orderInfo.GuestCheckoutTokenHash != "" {
		if subtle.ConstantTimeCompare([]byte(orderInfo.GuestCheckoutTokenHash), []byte(order.CheckoutTokenHash(checkoutToken))) != 1 {
			return xerr.Errorf(xerr.InvalidAccess, "guest checkout token is invalid")
		}
		return nil
	}
	// Compatibility for guest orders created before checkout capabilities were
	// persisted on the order itself.
	if s.deps.GuestCheckoutCache == nil {
		return xerr.Errorf(xerr.InvalidAccess, "guest checkout token is invalid")
	}
	value, err := s.deps.GuestCheckoutCache.Get(ctx, fmt.Sprintf(order.TempOrderCacheKey, orderInfo.OrderNo)).Result()
	if err != nil {
		return xerr.Errorf(xerr.InvalidAccess, "guest checkout token is invalid")
	}
	var tempOrder order.TemporaryOrderInfo
	if err := tempOrder.Unmarshal([]byte(value)); err != nil {
		return xerr.Errorf(xerr.InvalidAccess, "guest checkout token is invalid")
	}
	if tempOrder.OrderNo != orderInfo.OrderNo || tempOrder.CheckoutToken == "" ||
		subtle.ConstantTimeCompare([]byte(tempOrder.CheckoutToken), []byte(checkoutToken)) != 1 {
		return xerr.Errorf(xerr.InvalidAccess, "guest checkout token is invalid")
	}
	return nil
}

// payThroughGateway starts the order's payment at its gateway. The gateway
// is asked for exactly the payment expectation recorded on the order, so
// the callback it sends back always matches.
func (s *Service) payThroughGateway(ctx context.Context, orderInfo *order.Order, method *payment.Payment, returnURL string) (*dto.CheckoutOrderResponse, error) {
	gw, err := s.deps.Gateways.Open(method)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "open %s gateway", method.Platform)
	}
	// The callback is resolved before the payment expectation is recorded:
	// an order with a recorded expectation counts as sent to the gateway
	// when it closes.
	var notifyURL string
	if gw.NeedsNotifyURL(orderInfo) {
		if notifyURL, err = gateway.NotifyURL(method, s.siteHost()); err != nil {
			logger.WithContext(ctx).Errorw("[PurchaseCheckout] payment notify URL is not configured; set the payment method domain or the site host",
				logger.Field("payment", method.Id), logger.Field("platform", method.Platform))
			return nil, err
		}
	}
	charge, err := s.expectedCharge(ctx, gw, orderInfo)
	if err != nil {
		return nil, err
	}
	resp, err := gw.StartPayment(ctx, gateway.Checkout{
		Order:     orderInfo,
		Charge:    charge,
		NotifyURL: notifyURL,
		ReturnURL: returnURL,
		Subject:   s.siteName(),
		Payer:     gateway.Payer{UserID: orderInfo.UserId},
		Claim:     s.claimTrade(orderInfo),
		BeginSession: func(ctx context.Context) (bool, error) {
			return s.deps.Orders.BeginPaymentSession(ctx, orderInfo.OrderNo)
		},
		CompleteSession: func(ctx context.Context, tradeNo, session string) (bool, error) {
			return s.deps.Orders.CompletePaymentSession(ctx, orderInfo.OrderNo, tradeNo, session)
		},
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "start %s payment of order %s", method.Platform, orderInfo.OrderNo)
	}
	return resp, nil
}

// expectedCharge is what the gateway must be asked for: the expectation the
// order's first checkout recorded, or a new one computed and recorded now.
// Reusing the record keeps a retry on the same amount even when the exchange
// rate moved in between.
func (s *Service) expectedCharge(ctx context.Context, gw gateway.Gateway, orderInfo *order.Order) (gateway.Charge, error) {
	if orderInfo.PaymentCurrency != "" {
		return s.recordedCharge(gw, orderInfo)
	}
	charge, err := gateway.ChargeFor(ctx, gw, orderInfo.Amount, s.currencyUnit(), exchangerate.Source{
		Cache: s.deps.ExchangeRate, AccessKey: s.currencyAccess(),
	})
	if err != nil {
		return gateway.Charge{}, xerr.Wrapf(err, xerr.ERROR, "price the payment of order %s", orderInfo.OrderNo)
	}
	updated, err := s.deps.Orders.UpdatePaymentExpectation(ctx, orderInfo.OrderNo, charge.Amount, charge.Currency)
	if err != nil {
		return gateway.Charge{}, xerr.Wrapf(err, xerr.DatabaseUpdateError, "save payment expectation of order %s", orderInfo.OrderNo)
	}
	if updated {
		orderInfo.PaymentAmount, orderInfo.PaymentCurrency = charge.Amount, charge.Currency
		return charge, nil
	}
	// A concurrent checkout recorded the immutable expectation first. Adopt
	// it (and, for Stripe or Cryptomus, the payment it claimed) so identical
	// retries continue on the one payment the order has.
	latest, err := s.deps.Orders.FindOneByOrderNo(ctx, orderInfo.OrderNo)
	if err != nil {
		return gateway.Charge{}, xerr.Wrapf(err, xerr.DatabaseQueryError, "reload payment expectation of order %s", orderInfo.OrderNo)
	}
	if !order.CanCheckout(latest.Status) {
		return gateway.Charge{}, xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
	}
	if latest.PaymentCurrency == "" {
		return gateway.Charge{}, xerr.Errorf(xerr.OrderStatusError, "payment checkout is being initialized; retry")
	}
	orderInfo.PaymentAmount, orderInfo.PaymentCurrency, orderInfo.TradeNo = latest.PaymentAmount, latest.PaymentCurrency, latest.TradeNo
	return s.recordedCharge(gw, orderInfo)
}

// recordedCharge returns the order's recorded expectation after checking it
// is in the currency the gateway collects.
func (s *Service) recordedCharge(gw gateway.Gateway, orderInfo *order.Order) (gateway.Charge, error) {
	currency := gw.ChargeCurrency()
	if currency == "" {
		currency = s.currencyUnit()
	}
	if !strings.EqualFold(orderInfo.PaymentCurrency, currency) {
		return gateway.Charge{}, xerr.Errorf(xerr.OrderStatusError, "payment expectation does not match existing checkout")
	}
	return gateway.Charge{Amount: orderInfo.PaymentAmount, Currency: strings.ToUpper(orderInfo.PaymentCurrency)}, nil
}

// claimTrade binds a provider-side payment as the order's only one. When a
// concurrent checkout claimed first, the winner's payment is returned.
func (s *Service) claimTrade(orderInfo *order.Order) func(context.Context, string) (string, error) {
	return func(ctx context.Context, tradeNo string) (string, error) {
		claimed, err := s.deps.Orders.SetPaymentTradeNoIfEmpty(ctx, orderInfo.OrderNo, tradeNo)
		if err != nil {
			return "", xerr.Wrapf(err, xerr.DatabaseUpdateError, "claim payment %s of order %s", tradeNo, orderInfo.OrderNo)
		}
		if claimed {
			orderInfo.TradeNo = tradeNo
			return tradeNo, nil
		}
		latest, err := s.deps.Orders.FindOneByOrderNo(ctx, orderInfo.OrderNo)
		if err != nil {
			return "", xerr.Wrapf(err, xerr.DatabaseQueryError, "reload order %s", orderInfo.OrderNo)
		}
		if !order.CanCheckout(latest.Status) || latest.TradeNo == "" {
			return "", xerr.Errorf(xerr.OrderStatusError, "order no longer has a pending gateway payment")
		}
		orderInfo.TradeNo = latest.TradeNo
		return latest.TradeNo, nil
	}
}

// payWithBalance pays the order from the buyer's wallet. Once the debit has
// committed the order is paid, and that committed outcome is what the buyer
// is told: an activation that cannot be queued now is re-driven by the
// paid-order reconciler.
func (s *Service) payWithBalance(ctx context.Context, orderInfo *order.Order) (*dto.CheckoutOrderResponse, error) {
	// A top-up must bring money in from outside the wallet. The balance
	// checkout spends gift credit first, so paying a recharge with it would
	// turn gift credit into regular balance.
	if orderInfo.Type == order.TypeRecharge {
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "balance cannot pay for a recharge")
	}
	if orderInfo.UserId == 0 {
		return nil, xerr.Errorf(xerr.UserNotExist, "user not found")
	}
	if err := s.debitBalance(ctx, orderInfo); err != nil {
		return nil, err
	}
	log := logger.WithContext(ctx)
	if err := s.deps.Queue.EnqueueActivation(ctx, orderInfo.OrderNo); err != nil {
		log.Errorw("[PurchaseCheckout] Enqueue activation failed; the paid-order reconciler retries it",
			logger.Field("error", err.Error()), logger.Field("orderNo", orderInfo.OrderNo))
	}
	log.Infow("[PurchaseCheckout] Balance payment completed", logger.Field("orderNo", orderInfo.OrderNo), logger.Field("userId", orderInfo.UserId))
	return &dto.CheckoutOrderResponse{Type: "balance"}, nil
}

// debitBalance marks the order paid and takes its amount from the wallet,
// gift credit first. A free order is marked paid without touching the
// wallet.
//
// The order's money columns keep the meaning pricing gave them at creation:
// Amount is what was paid with money and GiftAmount the gift credit the
// order consumed. Gift credit spent here therefore moves from Amount into
// GiftAmount instead of being added on top, so a refund, which returns
// Amount to the balance and GiftAmount to the gift balance, pays back
// exactly what was paid and RefundBasis (their sum) counts it once.
func (s *Service) debitBalance(ctx context.Context, o *order.Order) error {
	if o.Amount == 0 {
		updated, err := s.deps.Orders.UpdateOrderStatusFrom(ctx, o.OrderNo, order.StatusPending, order.StatusPaid)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "mark order %s paid", o.OrderNo)
		}
		if !updated {
			return xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
		}
		return nil
	}
	var paid *walletEntity.Wallet
	err := s.deps.Tx.InBillingTx(ctx, func(tx repository.BillingStore) error {
		// Lock the order first so concurrent checkout requests for the same
		// order cannot both reach the debit.
		current, err := tx.Order().FindOneByOrderNoForUpdate(ctx, o.OrderNo)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "lock order %s", o.OrderNo)
		}
		if !order.CanCheckout(current.Status) {
			return xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
		}
		// Read the wallet under its row lock, never from a cached user.
		wallet, err := tx.Wallet().FindOneForUpdate(ctx, o.UserId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "lock wallet of user %d", o.UserId)
		}
		if available := wallet.Balance + wallet.GiftAmount; available < current.Amount {
			return xerr.Errorf(xerr.InsufficientBalance, "Insufficient balance: required %d, available %d", current.Amount, available)
		}
		// The debit is priced from the locked row, not the caller's snapshot.
		giftUsed := min(wallet.GiftAmount, current.Amount)
		balanceUsed := current.Amount - giftUsed
		wallet.GiftAmount -= giftUsed
		wallet.Balance -= balanceUsed
		if err := tx.Wallet().UpdateBalanceFields(ctx, wallet); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "debit wallet of user %d", o.UserId)
		}
		if giftUsed > 0 {
			if err := ledger.SpendGift(ctx, tx.Log(), ledger.Gift{
				UserID: wallet.UserId, OrderNo: o.OrderNo, Amount: giftUsed, Balance: wallet.GiftAmount, Remark: ledger.RemarkBalancePayment,
			}); err != nil {
				return err
			}
			// The gift credit spent here moves from the amount paid with
			// money to the order's gift credit, alongside what was reserved
			// at creation; each unit is then refunded from one column only.
			current.GiftAmount += giftUsed
			current.Amount -= giftUsed
			if err := tx.Order().Update(ctx, current); err != nil {
				return xerr.Wrapf(err, xerr.DatabaseUpdateError, "record gift credit of order %s", o.OrderNo)
			}
			o.GiftAmount, o.Amount = current.GiftAmount, current.Amount
		}
		if balanceUsed > 0 {
			if err := ledger.PayWithBalance(ctx, tx.Log(), ledger.Balance{
				UserID: wallet.UserId, OrderNo: o.OrderNo, Amount: balanceUsed, Balance: wallet.Balance,
			}); err != nil {
				return err
			}
		}
		updated, err := tx.Order().UpdateOrderStatusFrom(ctx, o.OrderNo, order.StatusPending, order.StatusPaid)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "mark order %s paid", o.OrderNo)
		}
		if !updated {
			return xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
		}
		paid = wallet
		return nil
	})
	if err != nil {
		return err
	}
	if s.deps.UserCache != nil {
		if err := s.deps.UserCache.ClearUserCache(ctx, paid.UserId); err != nil {
			logger.WithContext(ctx).Errorw("[PurchaseCheckout] Clear user cache error", logger.Field("error", err.Error()), logger.Field("userId", paid.UserId))
		}
	}
	return nil
}
