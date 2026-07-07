package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/user"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type UnsubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUnsubscribeLogic creates a new instance of UnsubscribeLogic for handling subscription cancellation
func NewUnsubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsubscribeLogic {
	return &UnsubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Unsubscribe handles the subscription cancellation process with proper refund distribution
// It prioritizes refunding to gift amount for balance-paid orders, then to regular balance
func (l *UnsubscribeLogic) Unsubscribe(req *types.UnsubscribeRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// find user subscription by ID
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.Id)
	if err != nil {
		l.Errorw("FindOneSubscribe failed", logger.Field("error", err.Error()), logger.Field("reqId", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribe failed: %v", err.Error())
	}

	activate := []uint8{0, 1, 2}

	if !tool.Contains(activate, userSub.Status) {
		// Only active (2) or paused (5) subscriptions can be cancelled
		l.Errorw("Subscription status invalid for cancellation", logger.Field("userSubscribeId", userSub.Id), logger.Field("status", userSub.Status))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Subscription status invalid for cancellation")
	}

	// Calculate the remaining amount to refund based on unused subscription time/traffic
	remainingAmount, err := CalculateRemainingAmount(l.ctx, l.svcCtx, req.Id)
	if err != nil {
		return err
	}

	// Process unsubscription in a database transaction to ensure data consistency
	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Find and update subscription status to cancelled (status = 4)
		userSub.Status = 4 // Set status to cancelled
		if err = store.User().UpdateSubscribe(l.ctx, userSub); err != nil {
			return err
		}

		// Query the original order information to determine refund strategy
		orderInfo, err := store.Order().FindOne(l.ctx, userSub.OrderId)
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
				balance = u.Balance // Regular balance remains unchanged
			} else {
				// Gift amount insufficient - refund to gift first, remainder to regular balance
				gift = orderInfo.GiftAmount
				balance = u.Balance + (remainingAmount - orderInfo.GiftAmount)
			}
		} else {
			// For non-balance payment orders, refund entirely to regular balance
			balance = remainingAmount + u.Balance
			gift = 0
		}

		// Create balance log entry only if there's an actual regular balance refund
		balanceRefundAmount := balance - u.Balance
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
				ObjectID: u.Id,
				Content:  string(content),
			}); err != nil {
				return err
			}
		}

		// Create gift amount log entry if there's a gift balance refund
		if gift > 0 {

			giftLog := log.Gift{
				SubscribeId: userSub.Id,
				OrderNo:     orderInfo.OrderNo,
				Type:        log.GiftTypeIncrease, // Type 1 represents gift amount increase
				Amount:      gift,
				Balance:     u.GiftAmount + gift,
				Remark:      "Unsubscribe refund",
			}
			content, _ := giftLog.Marshal()

			if err := store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: u.Id,
				Content:  string(content),
			}); err != nil {
				return err
			}
			// Update user's gift amount
			u.GiftAmount += gift
		}

		// Update user's regular balance and save changes to database
		u.Balance = balance
		return store.User().Update(l.ctx, u)
	})

	if err != nil {
		l.Errorw("Unsubscribe transaction failed", logger.Field("error", err.Error()), logger.Field("userId", u.Id), logger.Field("reqId", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unsubscribe transaction failed: %v", err.Error())
	}

	//clear user subscription cache
	if err = l.svcCtx.Store.User().ClearSubscribeCache(l.ctx, userSub); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("userSubscribeId", userSub.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}
	// Clear subscription cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Errorw("ClearSubscribeCache failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	return err
}
