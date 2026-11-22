package wallet

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
)

// quotaGiftConsumer is the inbox consumer of the gift stage of an
// admin-scheduled quota task, keyed by (task, subscription). It is a
// persisted identity: renaming it would credit committed gifts again.
const quotaGiftConsumer = "billing.quota_gift"

// quotaGiftKey identifies one subscription's gift within one task run.
func quotaGiftKey(taskID, subscriptionID int64) string {
	return fmt.Sprintf("%d:%d", taskID, subscriptionID)
}

// QuotaGiftCredited reports whether the quota task's gift for the
// subscription was credited.
func (s *Service) QuotaGiftCredited(ctx context.Context, taskID, subscriptionID int64) (bool, error) {
	mark, err := s.deps.Store.Inbox().Find(ctx, quotaGiftConsumer, quotaGiftKey(taskID, subscriptionID))
	if err != nil {
		return false, err
	}
	return mark != nil, nil
}

// CreditQuotaGift is the billing stage of a quota task's grant to one
// subscription, run after the subscription module committed the grant: in
// one billing-domain transaction it credits amount to the gift balance of
// the subscription's owner with a gift log dated at and records the marker,
// exactly once per (task, subscription). A zero amount only records the
// marker.
func (s *Service) CreditQuotaGift(ctx context.Context, taskID, subscriptionID, userID, amount int64, at time.Time) error {
	key := quotaGiftKey(taskID, subscriptionID)
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		mark, err := store.Inbox().Find(ctx, quotaGiftConsumer, key)
		if err != nil {
			return err
		}
		if mark != nil {
			return nil
		}

		if amount > 0 {
			wallet, err := store.Wallet().FindOneForUpdate(ctx, userID)
			if err != nil {
				return fmt.Errorf("find wallet for user %d: %w", userID, err)
			}
			if amount > 0 && wallet.GiftAmount > math.MaxInt64-amount {
				return fmt.Errorf("gift balance overflows for user %d", userID)
			}
			wallet.GiftAmount += amount
			if err := store.Wallet().UpdateBalanceFields(ctx, wallet); err != nil {
				return fmt.Errorf("update user gift amount: %w", err)
			}
			if err := createQuotaGiftLog(ctx, store.Log(), subscriptionID, wallet.UserId, amount, wallet.GiftAmount, at); err != nil {
				return fmt.Errorf("create gift log: %w", err)
			}
		}

		return store.Inbox().Insert(ctx, quotaGiftConsumer, key, "")
	})
}

func createQuotaGiftLog(ctx context.Context, logs repository.LogRepo, subscribeId, userId, amount, balance int64, now time.Time) error {
	giftLog := &log.Gift{
		Type:        log.GiftTypeIncrease,
		OrderNo:     "",
		SubscribeId: subscribeId,
		Amount:      amount,
		Balance:     balance,
		Remark:      "Quota task gift",
		Timestamp:   now.UnixMilli(),
	}

	logString, err := giftLog.Marshal()
	if err != nil {
		return fmt.Errorf("marshal gift log error: %w", err)
	}
	return logs.Insert(ctx, &log.SystemLog{
		Type:     log.TypeGift.Uint8(),
		Content:  string(logString),
		ObjectID: userId,
		Date:     now.Format(time.DateOnly),
	})
}
