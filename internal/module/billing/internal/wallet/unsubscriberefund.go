package wallet

import (
	"context"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// unsubscribeRefundConsumer is the inbox consumer of the refund stage of a
// subscription cancellation (ADR-001 step 2), keyed by user-subscription id.
// It is a persisted identity: renaming it would settle committed refunds
// again.
const unsubscribeRefundConsumer = "billing.unsubscribe_refund"

func unsubscribeRefundKey(subscriptionID int64) string {
	return strconv.FormatInt(subscriptionID, 10)
}

// UnsubscribeRefundSettled reports whether the refund of the cancelled user
// subscription was settled.
func (s *Service) UnsubscribeRefundSettled(ctx context.Context, subscriptionID int64) (bool, error) {
	refunded, err := s.deps.Store.Inbox().Find(ctx, unsubscribeRefundConsumer, unsubscribeRefundKey(subscriptionID))
	if err != nil {
		return false, err
	}
	return refunded != nil, nil
}

// SettleUnsubscribeRefund is the billing stage of a subscription
// cancellation, run after the subscription module committed the
// cancellation: in one billing-domain transaction it credits amount, the
// unused share the cancellation recorded, to the buyer's wallet (gift amount
// first for balance-paid orders, then regular balance), takes back the
// referral commission the order earned and records the refund marker. A
// second settlement of the same subscription fails on the marker.
func (s *Service) SettleUnsubscribeRefund(ctx context.Context, userID, subscriptionID, orderID, amount int64) error {
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		// Subscriptions created by an administrator have no associated order.
		// They can be cancelled, but there is no payment to refund.
		if orderID != 0 {
			if err := s.refund(ctx, store, userID, subscriptionID, orderID, amount); err != nil {
				return err
			}
		}
		return store.Inbox().Insert(ctx, unsubscribeRefundConsumer, unsubscribeRefundKey(subscriptionID), "")
	})
}

// refund credits the order's refund to the buyer's wallet, logs the movement
// and reverses the referral commission the order earned.
func (s *Service) refund(ctx context.Context, store repository.BillingStore, userID, subID, orderID, remainingAmount int64) error {
	lockedUser, err := store.Wallet().FindOneForUpdate(ctx, userID)
	if err != nil {
		return err
	}
	// Query the original order information to determine refund strategy
	orderInfo, err := store.Order().FindOneDetails(ctx, orderID)
	if err != nil {
		return err
	}
	// A refund never exceeds what was paid, whatever amount an older
	// cancellation marker recorded.
	paidCash, paidGift := orderInfo.RefundSources()
	remainingAmount = max(0, min(remainingAmount, paidCash+paidGift))
	// The gift credit the order consumed returns to the gift balance first,
	// the rest to the regular balance, whatever method paid the rest: a
	// gateway-paid order consumed its gift credit at creation just as a
	// balance-paid one did, and refunding that share as balance would turn
	// gift credit into money.
	gift := min(paidGift, remainingAmount)
	balance := lockedUser.Balance + (remainingAmount - gift)

	now := timeutil.Now()
	// Create balance log entry only if there's an actual regular balance refund
	balanceRefundAmount := balance - lockedUser.Balance
	if balanceRefundAmount > 0 {
		content, err := (&log.Balance{
			OrderNo:   orderInfo.OrderNo,
			Amount:    balanceRefundAmount,
			Type:      log.BalanceTypeRefund,
			Balance:   balance,
			Timestamp: now.UnixMilli(),
		}).Marshal()
		if err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "encode refund balance log for order %s", orderInfo.OrderNo)
		}
		if err := insertLog(ctx, store, log.TypeBalance, lockedUser.UserId, now, content); err != nil {
			return err
		}
	}

	// Create gift amount log entry if there's a gift balance refund
	if gift > 0 {
		content, err := (&log.Gift{
			SubscribeId: subID,
			OrderNo:     orderInfo.OrderNo,
			Type:        log.GiftTypeIncrease,
			Amount:      gift,
			Balance:     lockedUser.GiftAmount + gift,
			Remark:      "Unsubscribe refund",
			Timestamp:   now.UnixMilli(),
		}).Marshal()
		if err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "encode refund gift log for order %s", orderInfo.OrderNo)
		}
		if err := insertLog(ctx, store, log.TypeGift, lockedUser.UserId, now, content); err != nil {
			return err
		}
		lockedUser.GiftAmount += gift
	}

	// Update only financial fields so this refund cannot overwrite a
	// concurrent profile/auth update.
	lockedUser.Balance = balance
	if err := store.Wallet().UpdateBalanceFields(ctx, lockedUser); err != nil {
		return err
	}
	return s.reverseCommission(ctx, store, userID, orderInfo, remainingAmount)
}

// reverseCommission takes back the referral commission the refunded orders
// earned, in proportion to the refund, so recycled balance cannot farm
// commission through buy-and-refund loops. Each order is charged to the
// referrer credited when it settled: an administrator may have pointed the
// buyer at another referrer since, who never received it. A referrer who
// already withdrew it goes negative, which blocks withdrawals until it is
// earned back.
func (s *Service) reverseCommission(ctx context.Context, store repository.BillingStore, buyerID int64, details *order.Details, refund int64) error {
	basis := details.RefundBasis()
	if refund <= 0 || basis <= 0 {
		return nil
	}
	earned, err := s.commissionByReferrer(ctx, buyerID, details)
	if err != nil {
		return err
	}
	now := timeutil.Now()
	for _, credited := range earned {
		reversed := credited.amount
		if refund < basis {
			reversed = int64(float64(credited.amount) * float64(refund) / float64(basis))
		}
		if reversed <= 0 {
			continue
		}
		referer, err := store.Wallet().FindOneForUpdate(ctx, credited.refererID)
		if err != nil {
			return err
		}
		referer.Commission -= reversed
		if err := store.Wallet().UpdateCommission(ctx, referer); err != nil {
			return err
		}
		// Negative like withdrawals, so summed commission logs stay net.
		content, err := (&log.Commission{
			Type:      log.CommissionTypeRefund,
			Amount:    -reversed,
			OrderNo:   details.OrderNo,
			Balance:   referer.Commission,
			Timestamp: now.UnixMilli(),
		}).Marshal()
		if err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "encode commission refund log for order %s", details.OrderNo)
		}
		if err := insertLog(ctx, store, log.TypeCommission, referer.UserId, now, content); err != nil {
			return err
		}
	}
	return nil
}

// creditedCommission is the commission one referrer earned from an order and
// its paid renewals.
type creditedCommission struct {
	refererID int64
	amount    int64
}

// commissionByReferrer sums the commission of the order and its paid renewals
// per referrer credited, in the order they were first credited. Orders
// settled before the credited referrer was recorded on them
// (CommissionRefererId 0) are charged to the buyer's current referrer, the
// only referrer known for them; a buyer without one leaves them uncharged.
func (s *Service) commissionByReferrer(ctx context.Context, buyerID int64, details *order.Details) ([]creditedCommission, error) {
	earnings := []creditedCommission{{refererID: details.CommissionRefererId, amount: details.Commission}}
	for _, subOrder := range details.SubOrders {
		if subOrder.IsPaidRenewal() {
			earnings = append(earnings, creditedCommission{refererID: subOrder.CommissionRefererId, amount: subOrder.Commission})
		}
	}
	var credited []creditedCommission
	position := map[int64]int{}
	var currentReferer *int64
	for _, earning := range earnings {
		if earning.amount <= 0 {
			continue
		}
		refererID := earning.refererID
		if refererID == 0 {
			if currentReferer == nil {
				buyer, err := s.deps.Profiles.FindOne(ctx, buyerID)
				if err != nil {
					return nil, err
				}
				currentReferer = &buyer.RefererId
			}
			refererID = *currentReferer
		}
		if refererID == 0 {
			continue
		}
		if i, ok := position[refererID]; ok {
			credited[i].amount += earning.amount
			continue
		}
		position[refererID] = len(credited)
		credited = append(credited, creditedCommission{refererID: refererID, amount: earning.amount})
	}
	return credited, nil
}

// insertLog records one wallet movement in the system log, dated in the
// application's zone.
func insertLog(ctx context.Context, store repository.BillingStore, kind log.Type, userID int64, now time.Time, content []byte) error {
	return store.Log().Insert(ctx, &log.SystemLog{
		Type:     kind.Uint8(),
		Date:     now.Format(time.DateOnly),
		ObjectID: userID,
		Content:  string(content),
	})
}
