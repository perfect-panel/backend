package selfsub

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/entity/user"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
)

type UnsubscribeLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewUnsubscribeLogic creates a new instance of UnsubscribeLogic for handling subscription cancellation
func newUnsubscribeLogic(ctx context.Context, deps Deps) *UnsubscribeLogic {
	return &UnsubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

// Inbox consumers for the two unsubscribe stages (ADR-001 step 2), keyed by
// user-subscription id. The cancellation marker carries "orderID|remaining"
// so a replay can settle the refund without recomputing it.
const (
	unsubscribeCancelConsumer = "subscription.unsubscribe_cancel"
	unsubscribeRefundConsumer = "billing.unsubscribe_refund"
)

// Unsubscribe cancels the subscription in a subscription-domain transaction,
// then settles the refund in a billing-domain transaction (gift amount first
// for balance-paid orders, then regular balance). A crash between the two is
// repaired when the user retries: a Deducted subscription whose refund marker
// is missing resumes at the refund stage.
func (l *UnsubscribeLogic) Unsubscribe(req *dto.UnsubscribeRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// find user subscription by ID
	userSub, err := l.deps.UserSubs.FindOneSubscribe(l.ctx, req.Id)
	if err != nil {
		l.Errorw("FindOneSubscribe failed", logger.Field("error", err.Error()), logger.Field("reqId", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribe failed: %v", err.Error())
	}
	if userSub.UserId != u.Id {
		l.Errorw("User subscribe does not belong to current user",
			logger.Field("userSubscribeId", userSub.Id),
			logger.Field("userId", u.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "user subscribe does not belong to current user")
	}

	cancelable := []uint8{user.SubscribeStatusPending, user.SubscribeStatusActive, user.SubscribeStatusFinished}
	subKey := strconv.FormatInt(req.Id, 10)

	if !tool.Contains(cancelable, userSub.Status) {
		resumable, resumeErr := l.hasUnsettledRefund(userSub.Status, subKey)
		if resumeErr != nil {
			return resumeErr
		}
		if !resumable {
			l.Errorw("Subscription status invalid for cancellation", logger.Field("userSubscribeId", userSub.Id), logger.Field("status", userSub.Status))
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Subscription status invalid for cancellation")
		}
	} else {
		// Calculate the remaining amount to refund based on unused subscription time/traffic
		remainingAmount, err := CalculateRemainingAmount(l.ctx, l.deps, req.Id)
		if err != nil {
			return err
		}
		// Subscription-domain transaction: flip the status and durably record
		// what the billing stage owes.
		err = l.deps.Store.InSubscriptionTx(l.ctx, func(store repository.SubscriptionStore) error {
			// Re-read the subscription under a row lock. The context user is
			// only an authorization principal and can be stale.
			lockedSub, err := store.UserSubscription().FindOneSubscribeForUpdate(l.ctx, req.Id)
			if err != nil {
				return err
			}
			if lockedSub.UserId != u.Id {
				return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "user subscribe does not belong to current user")
			}
			if !tool.Contains(cancelable, lockedSub.Status) {
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Subscription status invalid for cancellation")
			}
			lockedSub.Status = user.SubscribeStatusDeducted
			if err = store.UserSubscription().UpdateSubscribe(l.ctx, lockedSub); err != nil {
				return err
			}
			return store.Inbox().Insert(l.ctx, unsubscribeCancelConsumer, subKey,
				fmt.Sprintf("%d|%d", lockedSub.OrderId, remainingAmount))
		})
		if err != nil {
			l.Errorw("Unsubscribe transaction failed", logger.Field("error", err.Error()), logger.Field("userId", u.Id), logger.Field("reqId", req.Id))
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unsubscribe transaction failed: %v", err.Error())
		}
	}

	// Billing-domain transaction: settle the refund exactly once.
	if err := l.settleRefundOnce(u.Id, req.Id, subKey); err != nil {
		l.Errorw("Unsubscribe refund failed", logger.Field("error", err.Error()), logger.Field("userId", u.Id), logger.Field("reqId", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unsubscribe refund failed: %v", err.Error())
	}

	//clear user subscription cache
	if err = l.deps.Cache.ClearSubscribeCache(l.ctx, userSub); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("userSubscribeId", userSub.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}
	// Clear subscription cache
	if err = l.deps.Plans.ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	return err
}

// hasUnsettledRefund reports whether a non-cancelable subscription is a
// Deducted one whose cancellation committed but whose refund never did.
func (l *UnsubscribeLogic) hasUnsettledRefund(status uint8, subKey string) (bool, error) {
	if status != user.SubscribeStatusDeducted {
		return false, nil
	}
	cancelled, err := l.deps.Inbox.Find(l.ctx, unsubscribeCancelConsumer, subKey)
	if err != nil {
		return false, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find cancellation marker failed: %v", err.Error())
	}
	if cancelled == nil {
		return false, nil
	}
	refunded, err := l.deps.Inbox.Find(l.ctx, unsubscribeRefundConsumer, subKey)
	if err != nil {
		return false, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find refund marker failed: %v", err.Error())
	}
	return refunded == nil, nil
}

// settleRefundOnce credits the refund recorded by the cancellation marker in
// a billing-domain transaction, guarded by the refund marker.
func (l *UnsubscribeLogic) settleRefundOnce(userID, subID int64, subKey string) error {
	cancelled, err := l.deps.Inbox.Find(l.ctx, unsubscribeCancelConsumer, subKey)
	if err != nil {
		return err
	}
	if cancelled == nil {
		return fmt.Errorf("cancellation marker missing for subscription %s", subKey)
	}
	refunded, err := l.deps.Inbox.Find(l.ctx, unsubscribeRefundConsumer, subKey)
	if err != nil {
		return err
	}
	if refunded != nil {
		return nil
	}
	orderID, remainingAmount, err := parseCancellationMarker(cancelled.Result)
	if err != nil {
		return err
	}
	return l.deps.Store.InBillingTx(l.ctx, func(store repository.BillingStore) error {
		// Subscriptions created by an administrator have no associated order.
		// They can be cancelled, but there is no payment to refund.
		if orderID != 0 {
			lockedUser, err := store.Wallet().FindOneForUpdate(l.ctx, userID)
			if err != nil {
				return err
			}
			// Query the original order information to determine refund strategy
			orderInfo, err := store.Order().FindOne(l.ctx, orderID)
			if err != nil {
				return err
			}
			// Calculate refund distribution based on payment method and gift amount priority
			var balance, gift int64
			if orderInfo.Method == "balance" {
				// For balance-paid orders, prioritize refunding to gift amount first
				if orderInfo.GiftAmount >= remainingAmount {
					// Gift amount covers the entire refund - refund all to gift balance
					gift = remainingAmount
					balance = lockedUser.Balance // Regular balance remains unchanged
				} else {
					// Gift amount insufficient - refund to gift first, remainder to regular balance
					gift = orderInfo.GiftAmount
					balance = lockedUser.Balance + (remainingAmount - orderInfo.GiftAmount)
				}
			} else {
				// For non-balance payment orders, refund entirely to regular balance
				balance = remainingAmount + lockedUser.Balance
				gift = 0
			}

			// Create balance log entry only if there's an actual regular balance refund
			balanceRefundAmount := balance - lockedUser.Balance
			if balanceRefundAmount > 0 {
				balanceLog := log.Balance{
					OrderNo:   orderInfo.OrderNo,
					Amount:    balanceRefundAmount,
					Type:      log.BalanceTypeRefund, // Type 4 represents refund transaction
					Balance:   balance,
					Timestamp: timeutil.Now().UnixMilli(),
				}
				content, _ := balanceLog.Marshal()

				if err := store.Log().Insert(l.ctx, &log.SystemLog{
					Type:     log.TypeBalance.Uint8(),
					Date:     timeutil.Now().Format(time.DateOnly),
					ObjectID: lockedUser.Id,
					Content:  string(content),
				}); err != nil {
					return err
				}
			}

			// Create gift amount log entry if there's a gift balance refund
			if gift > 0 {
				giftLog := log.Gift{
					SubscribeId: subID,
					OrderNo:     orderInfo.OrderNo,
					Type:        log.GiftTypeIncrease, // Type 1 represents gift amount increase
					Amount:      gift,
					Balance:     lockedUser.GiftAmount + gift,
					Remark:      "Unsubscribe refund",
				}
				content, _ := giftLog.Marshal()

				if err := store.Log().Insert(l.ctx, &log.SystemLog{
					Type:     log.TypeGift.Uint8(),
					Date:     timeutil.Now().Format(time.DateOnly),
					ObjectID: lockedUser.Id,
					Content:  string(content),
				}); err != nil {
					return err
				}
				// Update user's gift amount
				lockedUser.GiftAmount += gift
			}

			// Update only financial fields so this refund cannot overwrite a
			// concurrent profile/auth update.
			lockedUser.Balance = balance
			if err := store.Wallet().UpdateBalanceFields(l.ctx, lockedUser); err != nil {
				return err
			}
		}
		return store.Inbox().Insert(l.ctx, unsubscribeRefundConsumer, subKey, "")
	})
}

func parseCancellationMarker(result string) (orderID, remainingAmount int64, err error) {
	parts := strings.SplitN(result, "|", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("corrupt cancellation marker %q", result)
	}
	if orderID, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return 0, 0, fmt.Errorf("corrupt cancellation marker %q: %w", result, err)
	}
	if remainingAmount, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return 0, 0, fmt.Errorf("corrupt cancellation marker %q: %w", result, err)
	}
	return orderID, remainingAmount, nil
}
