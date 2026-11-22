// Package activation owns the paid-order activation workflow and implements
// its billing-domain stages: the recharge wallet credit, the referral
// commission and the final settlement. Each stage is idempotent (inbox marker
// or status CAS); Workflow sequences them with the stages other modules run.
// The task adapter only decodes a message and invokes the billing facade,
// and only the facade may reach this package.
package activation

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// The consumer names are historical (the stages once lived under the
// identity label); they must not change, or in-flight replays would
// re-execute committed stages.
const (
	inboxRecharge   = "identity.balance_recharge"
	inboxCommission = "identity.commission"
	// inboxUnfulfillableRefund marks the refund of a paid order whose
	// fulfillment the subscription domain refused for good.
	inboxUnfulfillableRefund = "billing.unfulfillable_refund"
)

// unfulfillableRefundRemark labels the gift movement of such a refund in the
// gift log.
const unfulfillableRefundRemark = "Unfulfillable order refund"

// ErrInvalidOrderStatus reports a lost Paid->Finished CAS: the order left
// the Paid state underneath the settlement.
var ErrInvalidOrderStatus = errors.New("invalid order status")

// ProfileReader is the read-only identity port resolving referral settings;
// the legacy user repository satisfies it structurally.
type ProfileReader interface {
	FindOne(ctx context.Context, id int64) (*user.User, error)
}

// Orders reads the order an activation stage works on.
type Orders interface {
	FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error)
}

// Store is the narrow persistence surface the activation stages need; the
// repository store satisfies it structurally.
type Store interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
	Inbox() repository.InboxRepo
	Wallet() repository.WalletRepo
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Orders Orders
	// Store carries the billing-scoped transactions, the wallet view and
	// the inbox markers.
	Store Store
	// Profiles resolves the buyer's referrer and the referrer's commission
	// settings from the identity domain.
	Profiles ProfileReader
	// InvitePolicy snapshots the runtime-mutable site-wide referral
	// fallback (percentage, first-purchase-only).
	InvitePolicy func() (percentage uint8, onlyFirstPurchase bool)
}

// Service is the activation entry point used by the billing facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// ActivateRecharge credits the order's amount to the buyer's wallet exactly
// once and returns the post-credit balance (re-read on replays) for the
// caller's notification.
func (s *Service) ActivateRecharge(ctx context.Context, orderNo string) (int64, error) {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return 0, err
	}
	mark, err := s.deps.Store.Inbox().Find(ctx, inboxRecharge, orderNo)
	if err != nil {
		return 0, err
	}
	if mark != nil {
		// Replayed delivery: the credit already committed; report the
		// current balance for the notification.
		w, err := s.deps.Store.Wallet().FindWallet(ctx, orderInfo.UserId)
		if err != nil {
			return 0, err
		}
		if w == nil {
			return 0, nil
		}
		return w.Balance, nil
	}
	var balance int64
	err = s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		var txErr error
		balance, txErr = s.rechargeTx(ctx, store, orderInfo)
		if txErr != nil {
			return txErr
		}
		return store.Inbox().Insert(ctx, inboxRecharge, orderNo, "")
	})
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (s *Service) rechargeTx(ctx context.Context, store repository.BillingStore, orderInfo *order.Order) (int64, error) {
	wallet, err := store.Wallet().FindOneForUpdate(ctx, orderInfo.UserId)
	if err != nil {
		return 0, err
	}
	wallet.Balance += orderInfo.Price
	if err := store.Wallet().UpdateBalanceFields(ctx, wallet); err != nil {
		return 0, err
	}
	if err := ledger.Recharge(ctx, store.Log(), ledger.Balance{
		UserID: wallet.UserId, OrderNo: orderInfo.OrderNo, Amount: orderInfo.Price, Balance: wallet.Balance,
	}); err != nil {
		return 0, err
	}
	return wallet.Balance, nil
}

// SettleOrderCommission credits the referral commission for a purchase or
// renewal exactly once. The inbox marker also covers the "no commission
// applies" outcome so replays skip the referrer lock entirely.
func (s *Service) SettleOrderCommission(ctx context.Context, orderNo string, buyerID int64) error {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	mark, err := s.deps.Store.Inbox().Find(ctx, inboxCommission, orderNo)
	if err != nil {
		return err
	}
	if mark != nil {
		return nil
	}
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		if err := s.handleCommissionTx(ctx, store, buyerID, orderInfo); err != nil {
			return err
		}
		return store.Inbox().Insert(ctx, inboxCommission, orderNo, "")
	})
}

func (s *Service) handleCommissionTx(ctx context.Context, store repository.BillingStore, buyerID int64, orderInfo *order.Order) error {
	if orderInfo.Type != order.TypeSubscribe && orderInfo.Type != order.TypeRenewal {
		return nil
	}
	buyer, err := s.deps.Profiles.FindOne(ctx, buyerID)
	if err != nil {
		return err
	}
	if buyer.RefererId == 0 {
		return nil
	}
	refererProfile, err := s.deps.Profiles.FindOne(ctx, buyer.RefererId)
	if err != nil {
		return err
	}
	referer, err := store.Wallet().FindOneForUpdate(ctx, buyer.RefererId)
	if err != nil {
		return err
	}
	percentage := refererProfile.ReferralPercentage
	onlyFirst := refererProfile.OnlyFirstPurchase != nil && *refererProfile.OnlyFirstPurchase
	if percentage == 0 {
		percentage, onlyFirst = s.deps.InvitePolicy()
		if percentage == 0 {
			return nil
		}
	}
	if onlyFirst {
		if !orderInfo.IsNew {
			return nil
		}
		// IsNew is fixed when the order is created, so every order opened
		// before the first payment claims it. The referrer's wallet lock above
		// serializes this check, leaving exactly one of them commissioned.
		commissioned, err := store.Order().HasCommissionedOrder(ctx, buyerID, orderInfo.OrderNo)
		if err != nil {
			return err
		}
		if commissioned {
			return nil
		}
	}
	amount := calculateCommission(orderInfo.Amount-orderInfo.FeeAmount, percentage)
	if amount <= 0 {
		return nil
	}
	referer.Commission += amount
	if err := store.Wallet().UpdateCommission(ctx, referer); err != nil {
		return err
	}
	// The order keeps what it earned and who earned it so a refund can take
	// it back from that referrer.
	if err := store.Order().SetCommission(ctx, orderInfo.OrderNo, amount, referer.UserId); err != nil {
		return err
	}
	commissionType := log.CommissionTypePurchase
	if orderInfo.Type == order.TypeRenewal {
		commissionType = log.CommissionTypeRenewal
	}
	content, err := (&log.Commission{
		Type:      commissionType,
		Amount:    amount,
		OrderNo:   orderInfo.OrderNo,
		Timestamp: orderInfo.CreatedAt.UnixMilli(),
	}).Marshal()
	if err != nil {
		return err
	}
	return store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeCommission.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: referer.UserId,
		Content:  string(content),
	})
}

// calculateCommission is percentage percent of price, rounded down to whole
// minor units. The float product it replaced under-paid a unit whenever the
// percentage has no exact binary fraction (29% of 100 came out as 28). A
// percentage above 100, which an older administration API accepted, pays the
// whole price at most: a commission must never exceed what the buyer paid.
func calculateCommission(price int64, percentage uint8) int64 {
	if percentage > maxCommissionPercentage {
		percentage = maxCommissionPercentage
	}
	return price * int64(percentage) / 100
}

// maxCommissionPercentage caps the referral percentage at the whole price.
const maxCommissionPercentage uint8 = 100

// UnfulfillableRefunded reports whether the order was refunded because the
// subscription domain could not fulfil it.
func (s *Service) UnfulfillableRefunded(ctx context.Context, orderNo string) (bool, error) {
	mark, err := s.deps.Store.Inbox().Find(ctx, inboxUnfulfillableRefund, orderNo)
	if err != nil {
		return false, err
	}
	return mark != nil, nil
}

// RefundUnfulfillable returns what a paid order collected when the
// subscription domain refuses to fulfil it for good (its subscription was
// refunded, stopped or handed to a payment provider between checkout and
// payment): the payment goes back to the buyer's balance and the gift credit
// the order held to the gift balance, each with the ledger entry a refund
// writes, a reserved coupon use is released as a close releases it, and the
// order ends Closed with its event. Everything commits in one billing
// transaction with the inbox marker, so a redelivered activation refunds
// once; the order row lock serializes concurrent deliveries.
func (s *Service) RefundUnfulfillable(ctx context.Context, orderNo string) error {
	return s.refundUnfulfillable(ctx, orderNo, 0)
}

// RefundUnfulfillableToAccount refunds a paid guest order whose identity
// already belongs to account accountID, so the order can open no account of
// its own: the buyer who paid twice under one identity, or paid for an
// identity registered meanwhile. The order is bound to that account and its
// payment returned to the account's wallet in the one transaction that
// closes it, so a redelivery finds it closed and refunded, never bound but
// unpaid back.
func (s *Service) RefundUnfulfillableToAccount(ctx context.Context, orderNo string, accountID int64) error {
	if accountID == 0 {
		return errors.New("an account is required to refund a guest order")
	}
	return s.refundUnfulfillable(ctx, orderNo, accountID)
}

// refundUnfulfillable is RefundUnfulfillable; a non-zero accountID first
// binds an order without an account to it.
func (s *Service) refundUnfulfillable(ctx context.Context, orderNo string, accountID int64) error {
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		mark, err := store.Inbox().Find(ctx, inboxUnfulfillableRefund, orderNo)
		if err != nil {
			return err
		}
		if mark != nil {
			return nil
		}
		current, err := store.Order().FindOneByOrderNoForUpdate(ctx, orderNo)
		if err != nil {
			return err
		}
		if current.Status != order.StatusPaid {
			return ErrInvalidOrderStatus
		}
		if accountID != 0 && current.UserId == 0 {
			current.UserId = accountID
			if err := store.Order().Update(ctx, current); err != nil {
				return err
			}
		}
		if current.UserId == 0 {
			return errors.New("the order has no account to refund to")
		}
		if err := s.refundTx(ctx, store, current); err != nil {
			return err
		}
		closed, err := store.Order().UpdateOrderStatusFrom(ctx, orderNo, order.StatusPaid, order.StatusClosed)
		if err != nil {
			return err
		}
		if !closed {
			return ErrInvalidOrderStatus
		}
		return store.Inbox().Insert(ctx, inboxUnfulfillableRefund, orderNo, "")
	})
}

// refundTx moves the order's money back under the wallet lock: Amount is
// what the buyer paid with money (a gateway charge or the wallet balance) and
// returns to the balance, as a cancellation refund returns it; GiftAmount is
// the gift credit the order consumed, at creation or at its balance checkout,
// and returns to the gift balance, as a close returns it. A balance checkout
// moves the gift credit it spends out of Amount, so the two never hold the
// same unit and the refund pays back exactly what was paid.
func (s *Service) refundTx(ctx context.Context, store repository.BillingStore, o *order.Order) error {
	wallet, err := store.Wallet().FindOneForUpdate(ctx, o.UserId)
	if err != nil {
		return err
	}
	now := timeutil.Now()
	if o.GiftAmount > 0 {
		wallet.GiftAmount += o.GiftAmount
		if err := ledger.RefundGift(ctx, store.Log(), ledger.Gift{
			UserID: wallet.UserId, OrderNo: o.OrderNo, Amount: o.GiftAmount, Balance: wallet.GiftAmount, Remark: unfulfillableRefundRemark,
		}); err != nil {
			return err
		}
	}
	if o.Amount > 0 {
		wallet.Balance += o.Amount
		content, err := (&log.Balance{
			Type: log.BalanceTypeRefund, Amount: o.Amount, OrderNo: o.OrderNo, Balance: wallet.Balance, Timestamp: now.UnixMilli(),
		}).Marshal()
		if err != nil {
			return err
		}
		if err := store.Log().Insert(ctx, &log.SystemLog{
			Type: log.TypeBalance.Uint8(), Date: now.Format(time.DateOnly), ObjectID: wallet.UserId, Content: string(content),
		}); err != nil {
			return err
		}
	}
	if o.GiftAmount > 0 || o.Amount > 0 {
		if err := store.Wallet().UpdateBalanceFields(ctx, wallet); err != nil {
			return err
		}
	}
	if o.Coupon != "" && o.CouponReserved {
		return store.Coupon().ReleaseUsage(ctx, o.Coupon)
	}
	return nil
}

// FinalizeOrder is the billing-domain settlement: coupon accounting and the
// Paid -> Finished transition (which appends the order.fulfilled outbox
// event) commit atomically. Losing the status CAS rolls the coupon count
// back, so it stays exactly-once.
//
// Known transitional window: an admin closing a Paid order between the
// fulfillment stage and this CAS leaves the fulfillment committed while the
// order ends Closed. The pre-split code had the same conflict resolved by
// row locks; compensation for admin closes of paid orders is a billing
// concern tracked in ADR-001 step 2.
func (s *Service) FinalizeOrder(ctx context.Context, orderNo string) error {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		if orderInfo.Coupon != "" && !orderInfo.CouponReserved {
			if err := store.Coupon().UpdateCount(ctx, orderInfo.Coupon); err != nil {
				return err
			}
		}
		updated, err := store.Order().UpdateOrderStatusFrom(ctx, orderInfo.OrderNo, order.StatusPaid, order.StatusFinished)
		if err != nil {
			return err
		}
		if !updated {
			return ErrInvalidOrderStatus
		}
		return nil
	})
}
