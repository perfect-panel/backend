// Package orderLogic provides order processing logic for handling various types of orders
// including subscription purchases, renewals, traffic resets, and balance recharges.
package orderLogic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/queue/types"
)

// Order type constants define the different types of orders that can be processed
const (
	OrderTypeSubscribe    = 1 // New subscription purchase
	OrderTypeRenewal      = 2 // Subscription renewal
	OrderTypeResetTraffic = 3 // Traffic quota reset
	OrderTypeRecharge     = 4 // Balance recharge
)

// Order status constants define the lifecycle states of an order
const (
	OrderStatusPending  = 1 // Order created but not paid
	OrderStatusPaid     = 2 // Order paid and ready for processing
	OrderStatusClose    = 3 // Order closed/cancelled
	OrderStatusFailed   = 4 // Order processing failed
	OrderStatusFinished = 5 // Order successfully completed
)

// Predefined error variables for common error conditions
var (
	ErrInvalidOrderStatus = fmt.Errorf("invalid order status")
	ErrInvalidOrderType   = fmt.Errorf("invalid order type")
)

// ActivateOrderLogic handles the activation and processing of paid orders
type ActivateOrderLogic struct {
	svc *svc.ServiceContext // Service context containing dependencies
}

// activationResult contains only post-commit work.  All financial and
// subscription mutations are committed with the order transition first; cache
// invalidation and notifications are deliberately kept outside that
// transaction because they are retryable side effects rather than settlement
// state.
type activationResult struct {
	order               *order.Order
	user                *user.User
	subscribe           *subscribe.Subscribe
	userSub             *user.Subscribe
	commissionRecipient *user.User
	notifyType          string
}

// NewActivateOrderLogic creates a new instance of ActivateOrderLogic
func NewActivateOrderLogic(svc *svc.ServiceContext) *ActivateOrderLogic {
	return &ActivateOrderLogic{
		svc: svc,
	}
}

// ProcessTask is the main entry point for processing order activation tasks.
// It handles the complete workflow of activating a paid order including validation,
// processing based on order type, and finalization.
func (l *ActivateOrderLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	payload, err := l.parsePayload(ctx, task.Payload())
	if err != nil {
		return err
	}

	var result *activationResult
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		// The Paid -> Finished transition, user/subscription mutations and coupon
		// accounting share one transaction.  This is the idempotency boundary for
		// Asynq's at-least-once delivery: a committed activation is never run
		// again, while a rolled-back activation has no partial database effects.
		orderInfo, err := store.Order().FindOneByOrderNoForUpdate(ctx, payload.OrderNo)
		if err != nil {
			return err
		}
		if orderInfo.Status == OrderStatusFinished {
			return nil
		}
		if orderInfo.Status != OrderStatusPaid {
			return ErrInvalidOrderStatus
		}

		result, err = l.processOrderByTypeInTx(ctx, store, orderInfo)
		if err != nil {
			return err
		}
		if orderInfo.Type == OrderTypeSubscribe || orderInfo.Type == OrderTypeRenewal {
			result.commissionRecipient, err = l.handleCommissionTx(ctx, store, result.user, orderInfo)
			if err != nil {
				return err
			}
		}
		if orderInfo.Coupon != "" && !orderInfo.CouponReserved {
			if err := store.Coupon().UpdateCount(ctx, orderInfo.Coupon); err != nil {
				return err
			}
		}
		updated, err := store.Order().UpdateOrderStatusFrom(ctx, orderInfo.OrderNo, OrderStatusPaid, OrderStatusFinished)
		if err != nil {
			return err
		}
		if !updated {
			return ErrInvalidOrderStatus
		}
		return nil
	})
	if err != nil {
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Process task failed", logger.Field("error", err.Error()))
		return err
	}
	if result == nil {
		return nil
	}
	l.afterActivationCommit(ctx, result)
	return nil
}

func (l *ActivateOrderLogic) processOrderByTypeInTx(ctx context.Context, store repository.Store, orderInfo *order.Order) (*activationResult, error) {
	switch orderInfo.Type {
	case OrderTypeSubscribe:
		return l.activateNewPurchaseTx(ctx, store, orderInfo)
	case OrderTypeRenewal:
		return l.activateRenewalTx(ctx, store, orderInfo)
	case OrderTypeResetTraffic:
		return l.activateResetTrafficTx(ctx, store, orderInfo)
	case OrderTypeRecharge:
		return l.activateRechargeTx(ctx, store, orderInfo)
	default:
		return nil, ErrInvalidOrderType
	}
}

func (l *ActivateOrderLogic) activateNewPurchaseTx(ctx context.Context, store repository.Store, orderInfo *order.Order) (*activationResult, error) {
	var (
		userInfo *user.User
		err      error
	)
	if orderInfo.UserId != 0 {
		// Serialise quota checks and subscription creation for a single user.
		userInfo, err = store.User().FindOneForUpdate(ctx, orderInfo.UserId)
		if err != nil {
			return nil, err
		}
	} else {
		tempOrder, err := l.getGuestOrderInfo(ctx, orderInfo)
		if err != nil {
			return nil, err
		}
		passwordHash := tempOrder.PasswordHash
		if passwordHash == "" {
			// Compatibility for an already-created guest checkout from an older
			// release. New records only retain PasswordHash in Redis.
			passwordHash = tool.EncodePassWord(tempOrder.Password)
		}
		if passwordHash == "" {
			return nil, fmt.Errorf("guest order password hash is missing")
		}
		userInfo = &user.User{Password: passwordHash, Algo: tool.PasswordAlgoForHash(passwordHash)}
		if err := store.User().Insert(ctx, userInfo); err != nil {
			return nil, err
		}
		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		if err := store.User().Update(ctx, userInfo); err != nil {
			return nil, err
		}
		if err := store.UserAuth().InsertUserAuthMethods(ctx, &user.AuthMethods{
			UserId:         userInfo.Id,
			AuthType:       tempOrder.AuthType,
			AuthIdentifier: tempOrder.Identifier,
		}); err != nil {
			return nil, err
		}
		if tempOrder.InviteCode != "" {
			if referer, findErr := store.User().FindOneByReferCode(ctx, tempOrder.InviteCode); findErr == nil {
				userInfo.RefererId = referer.Id
				if err := store.User().Update(ctx, userInfo); err != nil {
					return nil, err
				}
			} else {
				logger.WithContext(ctx).Error("Find referer failed", logger.Field("error", findErr.Error()), logger.Field("refer_code", tempOrder.InviteCode))
			}
		}
		orderInfo.UserId = userInfo.Id
		if err := store.Order().Update(ctx, orderInfo); err != nil {
			return nil, err
		}
	}

	sub, err := store.Subscribe().FindOne(ctx, orderInfo.SubscribeId)
	if err != nil {
		return nil, err
	}
	userSub, err := l.createUserSubscriptionTx(ctx, store, orderInfo, sub)
	if err != nil {
		return nil, err
	}
	return &activationResult{order: orderInfo, user: userInfo, subscribe: sub, userSub: userSub, notifyType: telegram.PurchaseNotify}, nil
}

func (l *ActivateOrderLogic) createUserSubscriptionTx(ctx context.Context, store repository.Store, orderInfo *order.Order, sub *subscribe.Subscribe) (*user.Subscribe, error) {
	if l.svc.Config.Subscribe.SingleModel {
		hasBlockingSubscription, err := store.UserSubscription().HasBlockingSubscription(ctx, orderInfo.UserId)
		if err != nil {
			return nil, err
		}
		if hasBlockingSubscription {
			return nil, fmt.Errorf("single subscription mode exceeds limit")
		}
	}
	if sub.Quota > 0 {
		count, err := store.UserSubscription().CountQuotaConsumingSubscriptions(ctx, orderInfo.UserId, orderInfo.SubscribeId)
		if err != nil {
			return nil, err
		}
		if count >= sub.Quota {
			return nil, fmt.Errorf("subscribe quota limit exceeded")
		}
	}
	now := timeutil.Now()
	userSub := &user.Subscribe{
		UserId:      orderInfo.UserId,
		OrderId:     orderInfo.Id,
		SubscribeId: orderInfo.SubscribeId,
		StartTime:   now,
		ExpireTime:  tool.AddTime(sub.UnitTime, orderInfo.Quantity, now),
		Traffic:     sub.Traffic,
		Token:       uuidx.SubscribeToken(orderInfo.OrderNo),
		UUID:        uuid.New().String(),
		Status:      1,
	}
	if err := store.UserSubscription().InsertSubscribe(ctx, userSub); err != nil {
		return nil, err
	}
	return userSub, nil
}

func (l *ActivateOrderLogic) activateRenewalTx(ctx context.Context, store repository.Store, orderInfo *order.Order) (*activationResult, error) {
	userInfo, err := store.User().FindOne(ctx, orderInfo.UserId)
	if err != nil {
		return nil, err
	}
	userSub, err := store.UserSubscription().FindOneSubscribeByTokenForUpdate(ctx, orderInfo.SubscribeToken)
	if err != nil {
		return nil, err
	}
	if userSub.UserId != orderInfo.UserId {
		return nil, fmt.Errorf("renewal subscription ownership mismatch")
	}
	sub, err := store.Subscribe().FindOne(ctx, orderInfo.SubscribeId)
	if err != nil {
		return nil, err
	}
	if err := l.updateSubscriptionForRenewalTx(ctx, store, userSub, sub, orderInfo); err != nil {
		return nil, err
	}
	return &activationResult{order: orderInfo, user: userInfo, subscribe: sub, userSub: userSub, notifyType: telegram.RenewalNotify}, nil
}

func (l *ActivateOrderLogic) updateSubscriptionForRenewalTx(ctx context.Context, store repository.Store, userSub *user.Subscribe, sub *subscribe.Subscribe, orderInfo *order.Order) error {
	now := timeutil.Now()
	if userSub.ExpireTime.Before(now) {
		userSub.ExpireTime = now
	}
	today := now.Day()
	resetDay := userSub.ExpireTime.Day()
	if (sub.RenewalReset != nil && *sub.RenewalReset) || today == resetDay {
		userSub.Download = 0
		userSub.Upload = 0
	}
	if userSub.FinishedAt != nil {
		if userSub.FinishedAt.Before(now) && today > resetDay {
			userSub.Download = 0
			userSub.Upload = 0
		}
		userSub.FinishedAt = nil
	}
	userSub.ExpireTime = tool.AddTime(sub.UnitTime, orderInfo.Quantity, userSub.ExpireTime)
	userSub.Status = 1
	return store.UserSubscription().UpdateSubscribe(ctx, userSub)
}

func (l *ActivateOrderLogic) activateResetTrafficTx(ctx context.Context, store repository.Store, orderInfo *order.Order) (*activationResult, error) {
	userInfo, err := store.User().FindOne(ctx, orderInfo.UserId)
	if err != nil {
		return nil, err
	}
	userSub, err := store.UserSubscription().FindOneSubscribeByTokenForUpdate(ctx, orderInfo.SubscribeToken)
	if err != nil {
		return nil, err
	}
	if userSub.UserId != orderInfo.UserId {
		return nil, fmt.Errorf("reset subscription ownership mismatch")
	}
	userSub.Download = 0
	userSub.Upload = 0
	userSub.Status = 1
	if err := store.UserSubscription().UpdateSubscribe(ctx, userSub); err != nil {
		return nil, err
	}
	sub, err := store.Subscribe().FindOne(ctx, userSub.SubscribeId)
	if err != nil {
		return nil, err
	}
	resetLog := &log.ResetSubscribe{
		Type:      log.ResetSubscribeTypePaid,
		UserId:    userInfo.Id,
		OrderNo:   orderInfo.OrderNo,
		Timestamp: timeutil.Now().UnixMilli(),
	}
	content, err := resetLog.Marshal()
	if err != nil {
		return nil, err
	}
	if err := store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: userSub.Id,
		Content:  string(content),
	}); err != nil {
		return nil, err
	}
	return &activationResult{order: orderInfo, user: userInfo, subscribe: sub, userSub: userSub, notifyType: telegram.ResetTrafficNotify}, nil
}

func (l *ActivateOrderLogic) activateRechargeTx(ctx context.Context, store repository.Store, orderInfo *order.Order) (*activationResult, error) {
	userInfo, err := store.User().FindOneForUpdate(ctx, orderInfo.UserId)
	if err != nil {
		return nil, err
	}
	userInfo.Balance += orderInfo.Price
	if err := store.User().UpdateBalanceFields(ctx, userInfo); err != nil {
		return nil, err
	}
	balanceLog := &log.Balance{
		Amount:    orderInfo.Price,
		Type:      log.BalanceTypeRecharge,
		OrderNo:   orderInfo.OrderNo,
		Balance:   userInfo.Balance,
		Timestamp: timeutil.Now().UnixMilli(),
	}
	content, err := balanceLog.Marshal()
	if err != nil {
		return nil, err
	}
	if err := store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeBalance.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: userInfo.Id,
		Content:  string(content),
	}); err != nil {
		return nil, err
	}
	return &activationResult{order: orderInfo, user: userInfo}, nil
}

func (l *ActivateOrderLogic) afterActivationCommit(ctx context.Context, result *activationResult) {
	if result == nil || result.order == nil || result.user == nil {
		return
	}
	switch result.order.Type {
	case OrderTypeSubscribe, OrderTypeRenewal, OrderTypeResetTraffic:
		if result.userSub != nil {
			if err := l.svc.Store.UserCache().ClearSubscribeCache(ctx, result.userSub); err != nil {
				logger.WithContext(ctx).Error("Clear user subscribe cache failed", logger.Field("error", err.Error()))
			}
		}
		if result.subscribe != nil {
			l.clearServerCache(ctx, result.subscribe)
		}
		if result.commissionRecipient != nil {
			if err := l.svc.Store.UserCache().UpdateUserCache(ctx, result.commissionRecipient); err != nil {
				logger.WithContext(ctx).Error("Update referer cache failed", logger.Field("error", err.Error()))
			}
		}
		if result.subscribe != nil {
			l.sendNotifications(ctx, result.order, result.user, result.subscribe, result.userSub, result.notifyType)
		}
	case OrderTypeRecharge:
		if err := l.svc.Store.UserCache().UpdateUserCache(ctx, result.user); err != nil {
			logger.WithContext(ctx).Error("[Recharge] Update user cache failed", logger.Field("error", err.Error()))
		}
		l.sendRechargeNotifications(ctx, result.order, result.user)
	}
}

// parsePayload unMarshals the task payload into a structured format
func (l *ActivateOrderLogic) parsePayload(ctx context.Context, payload []byte) (*types.ForthwithActivateOrderPayload, error) {
	var p types.ForthwithActivateOrderPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Unmarshal payload failed",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(payload)),
		)
		return nil, err
	}
	return &p, nil
}

// validateAndGetOrder retrieves an order by order number and validates its status
// Returns error if order is not found or not in paid status
func (l *ActivateOrderLogic) validateAndGetOrder(ctx context.Context, orderNo string) (*order.Order, error) {
	orderInfo, err := l.svc.Store.Order().FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		logger.WithContext(ctx).Error("Find order failed",
			logger.Field("error", err.Error()),
			logger.Field("order_no", orderNo),
		)
		return nil, err
	}

	if orderInfo.Status == OrderStatusFinished {
		logger.WithContext(ctx).Info("Order already finished, skip processing",
			logger.Field("order_no", orderInfo.OrderNo),
		)
		return nil, nil
	}

	if orderInfo.Status != OrderStatusPaid {
		logger.WithContext(ctx).Error("Order status error",
			logger.Field("order_no", orderInfo.OrderNo),
			logger.Field("status", orderInfo.Status),
		)
		return nil, ErrInvalidOrderStatus
	}

	return orderInfo, nil
}

// processOrderByType routes order processing based on the order type
func (l *ActivateOrderLogic) processOrderByType(ctx context.Context, orderInfo *order.Order) error {
	switch orderInfo.Type {
	case OrderTypeSubscribe:
		return l.NewPurchase(ctx, orderInfo)
	case OrderTypeRenewal:
		return l.Renewal(ctx, orderInfo)
	case OrderTypeResetTraffic:
		return l.ResetTraffic(ctx, orderInfo)
	case OrderTypeRecharge:
		return l.Recharge(ctx, orderInfo)
	default:
		logger.WithContext(ctx).Error("Order type is invalid", logger.Field("type", orderInfo.Type))
		return ErrInvalidOrderType
	}
}

// finalizeCouponAndOrder handles post-processing tasks including coupon updates
// and order status finalization
func (l *ActivateOrderLogic) finalizeCouponAndOrder(ctx context.Context, orderInfo *order.Order) {
	// Update coupon if exists
	if orderInfo.Coupon != "" {
		if err := l.svc.Store.Coupon().UpdateCount(ctx, orderInfo.Coupon); err != nil {
			logger.WithContext(ctx).Error("Update coupon status failed",
				logger.Field("error", err.Error()),
				logger.Field("coupon", orderInfo.Coupon),
			)
		}
	}

	// Update order status
	orderInfo.Status = OrderStatusFinished
	if err := l.svc.Store.Order().Update(ctx, orderInfo); err != nil {
		logger.WithContext(ctx).Error("Update order status failed",
			logger.Field("error", err.Error()),
			logger.Field("order_no", orderInfo.OrderNo),
		)
	}
}

// NewPurchase handles new subscription purchase including user creation,
// subscription setup, commission processing, cache updates, and notifications
func (l *ActivateOrderLogic) NewPurchase(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getUserOrCreate(ctx, orderInfo)
	if err != nil {
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, orderInfo.SubscribeId)
	if err != nil {
		return err
	}

	userSub, err := l.createUserSubscription(ctx, orderInfo, sub)
	if err != nil {
		return err
	}

	// Handle commission in separate goroutine to avoid blocking
	go l.handleCommission(context.Background(), userInfo, orderInfo)

	// Clear cache
	l.clearServerCache(ctx, sub)

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.PurchaseNotify)

	logger.WithContext(ctx).Info("Insert user subscribe success")
	return nil
}

// getUserOrCreate retrieves an existing user or creates a new guest user based on order details
func (l *ActivateOrderLogic) getUserOrCreate(ctx context.Context, orderInfo *order.Order) (*user.User, error) {
	if orderInfo.UserId != 0 {
		return l.getExistingUser(ctx, orderInfo.UserId)
	}
	return l.createGuestUser(ctx, orderInfo)
}

// getExistingUser retrieves user information by user ID
func (l *ActivateOrderLogic) getExistingUser(ctx context.Context, userId int64) (*user.User, error) {
	userInfo, err := l.svc.Store.User().FindOne(ctx, userId)
	if err != nil {
		logger.WithContext(ctx).Error("Find user failed",
			logger.Field("error", err.Error()),
			logger.Field("user_id", userId),
		)
		return nil, err
	}
	return userInfo, nil
}

// createGuestUser creates a new user account for guest orders using temporary order information
// stored in Redis cache
func (l *ActivateOrderLogic) createGuestUser(ctx context.Context, orderInfo *order.Order) (*user.User, error) {
	tempOrder, err := l.getGuestOrderInfo(ctx, orderInfo)
	if err != nil {
		return nil, err
	}

	userInfo := &user.User{
		Password: tool.EncodePassWord(tempOrder.Password),
		Algo:     tool.PasswordAlgoArgon2id,
	}

	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		if err := store.User().Insert(ctx, userInfo); err != nil {
			return err
		}

		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		if err := store.User().Update(ctx, userInfo); err != nil {
			return err
		}

		if err := store.UserAuth().InsertUserAuthMethods(ctx, &user.AuthMethods{
			UserId:         userInfo.Id,
			AuthType:       tempOrder.AuthType,
			AuthIdentifier: tempOrder.Identifier,
		}); err != nil {
			return err
		}

		orderInfo.UserId = userInfo.Id
		return store.Order().Update(ctx, orderInfo)
	})

	if err != nil {
		logger.WithContext(ctx).Error("Create user failed", logger.Field("error", err.Error()))
		return nil, err
	}

	// Handle referrer relationship
	l.handleReferrer(ctx, userInfo, tempOrder.InviteCode)

	logger.WithContext(ctx).Info("Create guest user success",
		logger.Field("user_id", userInfo.Id),
		logger.Field("identifier", tempOrder.Identifier),
		logger.Field("auth_type", tempOrder.AuthType),
	)

	return userInfo, nil
}

// getTempOrderInfo retrieves temporary order information from Redis cache
func (l *ActivateOrderLogic) getTempOrderInfo(ctx context.Context, orderNo string) (*constant.TemporaryOrderInfo, error) {
	cacheKey := fmt.Sprintf(constant.TempOrderCacheKey, orderNo)
	data, err := l.svc.Redis.Get(ctx, cacheKey).Result()
	if err != nil {
		logger.WithContext(ctx).Error("Get temp order cache failed",
			logger.Field("error", err.Error()),
			logger.Field("cache_key", cacheKey),
		)
		return nil, err
	}

	var tempOrder constant.TemporaryOrderInfo
	if err = tempOrder.Unmarshal([]byte(data)); err != nil {
		logger.WithContext(ctx).Error("Unmarshal temp order cache failed",
			logger.Field("error", err.Error()),
			logger.Field("cache_key", cacheKey),
		)
		return nil, err
	}

	return &tempOrder, nil
}

func (l *ActivateOrderLogic) getGuestOrderInfo(ctx context.Context, orderInfo *order.Order) (*constant.TemporaryOrderInfo, error) {
	if orderInfo.GuestAuthType != "" && orderInfo.GuestIdentifier != "" && orderInfo.GuestPasswordHash != "" {
		return &constant.TemporaryOrderInfo{
			OrderNo:      orderInfo.OrderNo,
			Identifier:   orderInfo.GuestIdentifier,
			AuthType:     orderInfo.GuestAuthType,
			PasswordHash: orderInfo.GuestPasswordHash,
			InviteCode:   orderInfo.GuestInviteCode,
		}, nil
	}
	return l.getTempOrderInfo(ctx, orderInfo.OrderNo)
}

// handleReferrer establishes referrer relationship if an invite code is provided
func (l *ActivateOrderLogic) handleReferrer(ctx context.Context, userInfo *user.User, inviteCode string) {
	if inviteCode == "" {
		return
	}

	referer, err := l.svc.Store.User().FindOneByReferCode(ctx, inviteCode)
	if err != nil {
		logger.WithContext(ctx).Error("Find referer failed",
			logger.Field("error", err.Error()),
			logger.Field("refer_code", inviteCode),
		)
		return
	}

	userInfo.RefererId = referer.Id
	if err = l.svc.Store.User().Update(ctx, userInfo); err != nil {
		logger.WithContext(ctx).Error("Update user referer failed",
			logger.Field("error", err.Error()),
			logger.Field("user_id", userInfo.Id),
		)
	}
}

// getSubscribeInfo retrieves subscription plan details by subscription ID
func (l *ActivateOrderLogic) getSubscribeInfo(ctx context.Context, subscribeId int64) (*subscribe.Subscribe, error) {
	sub, err := l.svc.Store.Subscribe().FindOne(ctx, subscribeId)
	if err != nil {
		logger.WithContext(ctx).Error("Find subscribe failed",
			logger.Field("error", err.Error()),
			logger.Field("subscribe_id", subscribeId),
		)
		return nil, err
	}
	return sub, nil
}

// createUserSubscription creates a new user subscription record based on order and subscription plan details
func (l *ActivateOrderLogic) createUserSubscription(ctx context.Context, orderInfo *order.Order, sub *subscribe.Subscribe) (*user.Subscribe, error) {
	if l.svc.Config.Subscribe.SingleModel {
		hasBlockingSubscription, err := l.svc.Store.UserSubscription().HasBlockingSubscription(ctx, orderInfo.UserId)
		if err != nil {
			return nil, err
		}
		if hasBlockingSubscription {
			return nil, fmt.Errorf("single subscription mode exceeds limit")
		}
	}
	now := timeutil.Now()
	userSub := &user.Subscribe{
		UserId:      orderInfo.UserId,
		OrderId:     orderInfo.Id,
		SubscribeId: orderInfo.SubscribeId,
		StartTime:   now,
		ExpireTime:  tool.AddTime(sub.UnitTime, orderInfo.Quantity, now),
		Traffic:     sub.Traffic,
		Download:    0,
		Upload:      0,
		Token:       uuidx.SubscribeToken(orderInfo.OrderNo),
		UUID:        uuid.New().String(),
		Status:      1,
	}

	if sub.Quota > 0 {
		count, err := l.svc.Store.UserSubscription().CountQuotaConsumingSubscriptions(ctx, orderInfo.UserId, orderInfo.SubscribeId)
		if err != nil {
			logger.WithContext(ctx).Error("Count user subscribe failed", logger.Field("error", err.Error()))
			return nil, err
		}
		if count >= sub.Quota {
			logger.WithContext(ctx).Info("Subscribe quota limit exceeded",
				logger.Field("user_id", orderInfo.UserId),
				logger.Field("subscribe_id", orderInfo.SubscribeId),
				logger.Field("quota", sub.Quota),
				logger.Field("current_count", count),
			)
			return nil, fmt.Errorf("subscribe quota limit exceeded")
		}
	}

	if err := l.svc.Store.UserSubscription().InsertSubscribe(ctx, userSub); err != nil {
		logger.WithContext(ctx).Error("Insert user subscribe failed", logger.Field("error", err.Error()))
		return nil, err
	}

	return userSub, nil
}

// handleCommission is retained for legacy direct callers. The queue activation
// flow uses handleCommissionTx so its order transition, balance and audit log
// commit atomically.
func (l *ActivateOrderLogic) handleCommission(ctx context.Context, userInfo *user.User, orderInfo *order.Order) {
	var recipient *user.User
	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		var txErr error
		recipient, txErr = l.handleCommissionTx(ctx, store, userInfo, orderInfo)
		return txErr
	})
	if err != nil {
		logger.WithContext(ctx).Error("Update referer commission failed", logger.Field("error", err.Error()))
		return
	}
	if recipient != nil {
		if err = l.svc.Store.UserCache().UpdateUserCache(ctx, recipient); err != nil {
			logger.WithContext(ctx).Error("Update referer cache failed",
				logger.Field("error", err.Error()),
				logger.Field("user_id", recipient.Id),
			)
		}
	}
}

func (l *ActivateOrderLogic) handleCommissionTx(ctx context.Context, store repository.Store, userInfo *user.User, orderInfo *order.Order) (*user.User, error) {
	if userInfo == nil || userInfo.RefererId == 0 || (orderInfo.Type != OrderTypeSubscribe && orderInfo.Type != OrderTypeRenewal) {
		return nil, nil
	}
	referer, err := store.User().FindOneForUpdate(ctx, userInfo.RefererId)
	if err != nil {
		return nil, err
	}
	percentage := referer.ReferralPercentage
	if percentage != 0 {
		if referer.OnlyFirstPurchase != nil && *referer.OnlyFirstPurchase && !orderInfo.IsNew {
			return nil, nil
		}
	} else {
		if l.svc.Config.Invite.ReferralPercentage == 0 || (l.svc.Config.Invite.OnlyFirstPurchase && !orderInfo.IsNew) {
			return nil, nil
		}
		percentage = uint8(l.svc.Config.Invite.ReferralPercentage)
	}
	amount := l.calculateCommission(orderInfo.Amount-orderInfo.FeeAmount, percentage)
	if amount <= 0 {
		return nil, nil
	}
	referer.Commission += amount
	if err := store.User().UpdateCommission(ctx, referer); err != nil {
		return nil, err
	}
	commissionType := log.CommissionTypePurchase
	if orderInfo.Type == OrderTypeRenewal {
		commissionType = log.CommissionTypeRenewal
	}
	content, err := (&log.Commission{
		Type:      commissionType,
		Amount:    amount,
		OrderNo:   orderInfo.OrderNo,
		Timestamp: orderInfo.CreatedAt.UnixMilli(),
	}).Marshal()
	if err != nil {
		return nil, err
	}
	if err := store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeCommission.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: referer.Id,
		Content:  string(content),
	}); err != nil {
		return nil, err
	}
	return referer, nil
}

// shouldProcessCommission determines if commission should be processed based on
// referrer existence, commission settings, and order type
func (l *ActivateOrderLogic) shouldProcessCommission(userInfo *user.User, isFirstPurchase bool) bool {
	if userInfo == nil || userInfo.RefererId == 0 {
		return false
	}

	referer, err := l.svc.Store.User().FindOne(context.Background(), userInfo.RefererId)
	if err != nil {
		logger.Errorw("Find referer failed",
			logger.Field("error", err.Error()),
			logger.Field("referer_id", userInfo.RefererId))
		return false
	}
	if referer == nil {
		return false
	}

	// use referer's custom settings if set
	if referer.ReferralPercentage > 0 {
		if referer.OnlyFirstPurchase != nil && *referer.OnlyFirstPurchase && !isFirstPurchase {
			return false
		}
		return true
	}

	// use global settings
	if l.svc.Config.Invite.ReferralPercentage == 0 {
		return false
	}
	if l.svc.Config.Invite.OnlyFirstPurchase && !isFirstPurchase {
		return false
	}

	return true
}

// calculateCommission computes the commission amount based on order price and referral percentage
func (l *ActivateOrderLogic) calculateCommission(price int64, percentage uint8) int64 {
	return int64(float64(price) * (float64(percentage) / 100))
}

// clearServerCache clears user list cache for all servers associated with the subscription
func (l *ActivateOrderLogic) clearServerCache(ctx context.Context, sub *subscribe.Subscribe) {
	if err := l.svc.Store.Subscribe().ClearCache(ctx, sub.Id); err != nil {
		logger.WithContext(ctx).Error("[Order Queue] Clear subscribe cache failed", logger.Field("error", err.Error()))
	}
}

// Renewal handles subscription renewal including subscription extension,
// traffic reset (if configured), commission processing, and notifications
func (l *ActivateOrderLogic) Renewal(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	userSub, err := l.getUserSubscription(ctx, orderInfo.SubscribeToken)
	if err != nil {
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, orderInfo.SubscribeId)
	if err != nil {
		return err
	}

	if err = l.updateSubscriptionForRenewal(ctx, userSub, sub, orderInfo); err != nil {
		return err
	}

	// Clear user subscription cache
	err = l.svc.Store.UserCache().ClearSubscribeCache(ctx, userSub)
	if err != nil {
		logger.WithContext(ctx).Error("Clear user subscribe cache failed",
			logger.Field("error", err.Error()),
			logger.Field("subscribe_id", userSub.Id),
			logger.Field("user_id", userInfo.Id),
		)
	}

	// Clear cache
	l.clearServerCache(ctx, sub)

	// Handle commission
	go l.handleCommission(context.Background(), userInfo, orderInfo)

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.RenewalNotify)

	return nil
}

// getUserSubscription retrieves user subscription by token
func (l *ActivateOrderLogic) getUserSubscription(ctx context.Context, token string) (*user.Subscribe, error) {
	userSub, err := l.svc.Store.UserSubscription().FindOneSubscribeByToken(ctx, token)
	if err != nil {
		logger.WithContext(ctx).Error("Find user subscribe failed", logger.Field("error", err.Error()))
		return nil, err
	}
	return userSub, nil
}

// updateSubscriptionForRenewal updates subscription details for renewal including
// expiration time extension and traffic reset if configured
func (l *ActivateOrderLogic) updateSubscriptionForRenewal(ctx context.Context, userSub *user.Subscribe, sub *subscribe.Subscribe, orderInfo *order.Order) error {
	now := timeutil.Now()
	if userSub.ExpireTime.Before(now) {
		userSub.ExpireTime = now
	}
	today := timeutil.Now().Day()
	resetDay := userSub.ExpireTime.Day()

	// Reset traffic if enabled
	if (sub.RenewalReset != nil && *sub.RenewalReset) || today == resetDay {
		userSub.Download = 0
		userSub.Upload = 0
	}

	if userSub.FinishedAt != nil {
		if userSub.FinishedAt.Before(now) && today > resetDay {
			// reset user traffic if finished at is before now
			userSub.Download = 0
			userSub.Upload = 0
		}

		userSub.FinishedAt = nil
	}

	userSub.ExpireTime = tool.AddTime(sub.UnitTime, orderInfo.Quantity, userSub.ExpireTime)
	userSub.Status = 1

	if err := l.svc.Store.UserSubscription().UpdateSubscribe(ctx, userSub); err != nil {
		logger.WithContext(ctx).Error("Update user subscribe failed", logger.Field("error", err.Error()))
		return err
	}

	return nil
}

// ResetTraffic handles traffic quota reset for existing subscriptions
func (l *ActivateOrderLogic) ResetTraffic(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	userSub, err := l.getUserSubscription(ctx, orderInfo.SubscribeToken)
	if err != nil {
		return err
	}

	// Reset traffic
	userSub.Download = 0
	userSub.Upload = 0
	userSub.Status = 1

	if err := l.svc.Store.UserSubscription().UpdateSubscribe(ctx, userSub); err != nil {
		logger.WithContext(ctx).Error("Update user subscribe failed", logger.Field("error", err.Error()))
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, userSub.SubscribeId)
	if err != nil {
		return err
	}

	// Clear user subscription cache
	err = l.svc.Store.UserCache().ClearSubscribeCache(ctx, userSub)
	if err != nil {
		logger.WithContext(ctx).Error("Clear user subscribe cache failed",
			logger.Field("error", err.Error()),
			logger.Field("subscribe_id", userSub.Id),
			logger.Field("user_id", userInfo.Id),
		)
	}

	// Clear cache
	l.clearServerCache(ctx, sub)

	// insert reset traffic log
	resetLog := &log.ResetSubscribe{
		Type:      log.ResetSubscribeTypePaid,
		UserId:    userInfo.Id,
		OrderNo:   orderInfo.OrderNo,
		Timestamp: timeutil.Now().UnixMilli(),
	}

	content, _ := resetLog.Marshal()
	if err = l.svc.Store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: userSub.Id,
		Content:  string(content),
	}); err != nil {
		logger.WithContext(ctx).Error("[Order Queue]Insert reset subscribe log failed", logger.Field("error", err.Error()))
	}

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.ResetTrafficNotify)

	return nil
}

// Recharge handles balance recharge orders including balance updates,
// transaction logging, and notifications
func (l *ActivateOrderLogic) Recharge(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	// Update balance in transaction
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		userInfo.Balance += orderInfo.Price
		if err = store.User().Update(ctx, userInfo); err != nil {
			return err
		}

		balanceLog := &log.Balance{
			Amount:    orderInfo.Price,
			Type:      log.BalanceTypeRecharge,
			OrderNo:   orderInfo.OrderNo,
			Balance:   userInfo.Balance,
			Timestamp: timeutil.Now().UnixMilli(),
		}
		content, _ := balanceLog.Marshal()

		return store.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeBalance.Uint8(),
			Date:     timeutil.Now().Format("2006-01-02"),
			ObjectID: userInfo.Id,
			Content:  string(content),
		})
	})

	if err != nil {
		logger.WithContext(ctx).Error("[Recharge] Database transaction failed", logger.Field("error", err.Error()))
		return err
	}

	// clear user cache
	if err = l.svc.Store.UserCache().UpdateUserCache(ctx, userInfo); err != nil {
		logger.WithContext(ctx).Error("[Recharge] Update user cache failed", logger.Field("error", err.Error()))
		return err
	}

	// Send notifications
	l.sendRechargeNotifications(ctx, orderInfo, userInfo)

	return nil
}

// sendNotifications sends both user and admin notifications for order completion
func (l *ActivateOrderLogic) sendNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User, sub *subscribe.Subscribe, userSub *user.Subscribe, notifyType string) {
	// Send user notification
	if telegramId, ok := findTelegram(userInfo); ok {
		templateData := l.buildUserNotificationData(orderInfo, sub, userSub)
		if text, err := tool.RenderTemplateToString(notifyType, templateData); err == nil {
			l.sendUserNotifyWithTelegram(telegramId, text)
		}
	}

	// Send admin notification
	adminData := l.buildAdminNotificationData(orderInfo, sub)
	if text, err := tool.RenderTemplateToString(telegram.AdminOrderNotify, adminData); err == nil {
		l.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// sendRechargeNotifications sends specific notifications for balance recharge orders
func (l *ActivateOrderLogic) sendRechargeNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User) {
	// Send user notification
	if telegramId, ok := findTelegram(userInfo); ok {
		templateData := map[string]string{
			"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
			"PaymentMethod": orderInfo.Method,
			"Time":          orderInfo.CreatedAt.Format("2006-01-02 15:04:05"),
			"Balance":       fmt.Sprintf("%.2f", float64(userInfo.Balance)/100),
		}
		if text, err := tool.RenderTemplateToString(telegram.RechargeNotify, templateData); err == nil {
			l.sendUserNotifyWithTelegram(telegramId, text)
		}
	}

	// Send admin notification
	adminData := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
		"SubscribeName": "余额充值",
		"OrderStatus":   "已支付",
		"OrderTime":     orderInfo.CreatedAt.Format("2006-01-02 15:04:05"),
		"PaymentMethod": orderInfo.Method,
	}
	if text, err := tool.RenderTemplateToString(telegram.AdminOrderNotify, adminData); err == nil {
		l.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// buildUserNotificationData creates template data for user notifications
func (l *ActivateOrderLogic) buildUserNotificationData(orderInfo *order.Order, sub *subscribe.Subscribe, userSub *user.Subscribe) map[string]string {
	data := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"SubscribeName": sub.Name,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
	}

	if userSub != nil {
		data["ExpireTime"] = userSub.ExpireTime.Format("2006-01-02 15:04:05")
		data["ResetTime"] = timeutil.Now().Format("2006-01-02 15:04:05")
	}

	return data
}

// buildAdminNotificationData creates template data for admin notifications
func (l *ActivateOrderLogic) buildAdminNotificationData(orderInfo *order.Order, sub *subscribe.Subscribe) map[string]string {
	subscribeName := sub.Name
	if orderInfo.Type == OrderTypeResetTraffic {
		subscribeName = "流量重置"
	}

	return map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"SubscribeName": subscribeName,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
		"OrderStatus":   "已支付",
		"OrderTime":     orderInfo.CreatedAt.Format("2006-01-02 15:04:05"),
		"PaymentMethod": orderInfo.Method,
	}
}

// sendUserNotifyWithTelegram sends a notification message to a user via Telegram
func (l *ActivateOrderLogic) sendUserNotifyWithTelegram(chatId int64, text string) {
	if !l.svc.Config.Telegram.EnableNotify {
		return
	}
	msg := tgbotapi.NewMessage(chatId, text)
	msg.ParseMode = "markdown"
	if _, err := l.svc.TelegramBot.Send(msg); err != nil {
		logger.Error("Send telegram user message failed", logger.Field("error", err.Error()))
	}
}

// sendAdminNotifyWithTelegram sends a notification message to all admin users via Telegram
func (l *ActivateOrderLogic) sendAdminNotifyWithTelegram(ctx context.Context, text string) {
	if !l.svc.Config.Telegram.EnableNotify {
		return
	}
	admins, err := l.svc.Store.User().QueryAdminUsers(ctx)
	if err != nil {
		logger.WithContext(ctx).Error("Query admin users failed", logger.Field("error", err.Error()))
		return
	}

	for _, admin := range admins {
		if telegramId, ok := findTelegram(admin); ok {
			msg := tgbotapi.NewMessage(telegramId, text)
			msg.ParseMode = "markdown"
			if _, err := l.svc.TelegramBot.Send(msg); err != nil {
				logger.WithContext(ctx).Error("Send telegram admin message failed", logger.Field("error", err.Error()))
			}
		}
	}
}

// findTelegram extracts Telegram chat ID from user authentication methods.
// Returns the chat ID and a boolean indicating if Telegram auth was found.
func findTelegram(u *user.User) (int64, bool) {
	for _, item := range u.AuthMethods {
		if item.AuthType == "telegram" {
			if telegramId, err := strconv.ParseInt(item.AuthIdentifier, 10, 64); err == nil {
				return telegramId, true
			}
		}
	}
	return 0, false
}
