package order

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type PurchaseLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

const (
	CloseOrderTimeMinutes = 15
)

// NewPurchaseLogic creates a new purchase logic instance for subscription purchase operations.
// It initializes the logger with context and sets up the service context for database operations.
func NewPurchaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PurchaseLogic {
	return &PurchaseLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Purchase processes new subscription purchase orders including validation, discount calculation,
// coupon processing, gift amount deduction, fee calculation, and order creation with database transaction.
// It handles the complete purchase workflow from user validation to order creation and task scheduling.
func (l *PurchaseLogic) Purchase(req *dto.PurchaseOrderRequest) (resp *dto.PurchaseOrderResponse, err error) {
	store := l.svcCtx.Store

	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	if req.Quantity <= 0 {
		l.Debugf("[Purchase] Quantity is less than or equal to 0, setting to 1")
		req.Quantity = 1
	}

	// Validate quantity limit
	if req.Quantity > MaxQuantity {
		l.Errorw("[Purchase] Quantity exceeds maximum limit", logger.Field("quantity", req.Quantity), logger.Field("max", MaxQuantity))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "quantity exceeds maximum limit of %d", MaxQuantity)
	}

	if l.svcCtx.Config.Subscribe.SingleModel {
		hasBlockingSubscription, err := store.User().HasBlockingSubscription(l.ctx, u.Id)
		if err != nil {
			l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check user subscription error: %v", err.Error())
		}
		if hasBlockingSubscription {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserSubscribeExist), "user has subscription")
		}
	}

	// find subscribe plan
	sub, err := store.Subscribe().FindOne(l.ctx, req.SubscribeId)

	if err != nil {
		l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	// check subscribe plan status
	if !*sub.Sell {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "subscribe not sell")
	}

	// check subscribe plan inventory
	if sub.Inventory == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
	}

	// check subscribe plan limit
	if sub.Quota > 0 {
		count, err := store.User().CountQuotaConsumingSubscriptions(l.ctx, u.Id, req.SubscribeId)
		if err != nil {
			l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id), logger.Field("subscribe_id", req.SubscribeId))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "count user subscriptions error: %v", err.Error())
		}
		if count >= sub.Quota {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "quota limit")
		}
	}

	var discount float64 = 1
	if sub.Discount != "" {
		var dis []dto.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	// discount amount
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	// Validate amount to prevent overflow
	if amount > MaxOrderAmount {
		l.Errorw("[Purchase] Order amount exceeds maximum limit",
			logger.Field("amount", amount),
			logger.Field("max", MaxOrderAmount),
			logger.Field("user_id", u.Id),
			logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
	}

	var coupon int64 = 0
	// Calculate the coupon deduction
	if req.Coupon != "" {
		couponInfo, err := store.Coupon().FindOneByCode(l.ctx, req.Coupon)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotExist), "coupon not found")
			}
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if err := ensureCouponEnabled(couponInfo); err != nil {
			return nil, err
		}
		if couponInfo.Count != 0 && couponInfo.Count <= couponInfo.UsedCount {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used")
		}
		couponSub := tool.StringToInt64Slice(couponInfo.Subscribe)
		if len(couponSub) > 0 && !tool.Contains(couponSub, req.SubscribeId) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}
		count, err := store.Order().CountUserCouponUsage(l.ctx, u.Id, req.Coupon)
		if err != nil {
			l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id), logger.Field("coupon", req.Coupon))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if couponInfo.UserLimit > 0 && count >= couponInfo.UserLimit {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon limit exceeded")
		}
		coupon = calculateCoupon(amount, couponInfo)
	}
	// Calculate the handling fee
	amount -= coupon
	// find payment method
	payment, err := store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %v", err.Error())
	}
	if err := ensurePaymentAvailable(payment); err != nil {
		return nil, err
	}
	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, payment)
		amount += feeAmount

		// Final validation after adding fee
		if amount > MaxOrderAmount {
			l.Errorw("[Purchase] Final order amount exceeds maximum limit after fee",
				logger.Field("amount", amount),
				logger.Field("max", MaxOrderAmount),
				logger.Field("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
		}
	}

	// query user is new purchase or renewal
	isNew, err := store.Order().IsUserEligibleForNewOrder(l.ctx, u.Id)
	if err != nil {
		l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user order error: %v", err.Error())
	}
	// create order
	orderInfo := &order.Order{
		UserId:         u.Id,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           1,
		Quantity:       req.Quantity,
		Price:          price,
		Amount:         amount,
		Discount:       discountAmount,
		GiftAmount:     0,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		FeeAmount:      feeAmount,
		Status:         1,
		IsNew:          isNew,
		SubscribeId:    req.SubscribeId,
	}
	// Database transaction
	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		// The request-context user is only an authentication snapshot. Lock and
		// re-read the account before reserving gift credit so two concurrent
		// orders cannot spend the same balance.
		lockedUser, e := txStore.User().FindOneForUpdate(l.ctx, u.Id)
		if e != nil {
			return e
		}

		if sub.Quota > 0 {
			count, e := txStore.User().CountQuotaConsumingSubscriptions(l.ctx, u.Id, req.SubscribeId)
			if e != nil {
				l.Errorw("[Purchase] Database query error", logger.Field("error", e.Error()), logger.Field("user_id", u.Id), logger.Field("subscribe_id", req.SubscribeId))
				return e
			}
			if count >= sub.Quota {
				return errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "quota limit")
			}
		}
		if orderInfo.Coupon != "" {
			reserved, e := txStore.Coupon().ReserveUsage(l.ctx, orderInfo.Coupon, timeutil.Now().Unix())
			if e != nil {
				return e
			}
			if !reserved {
				return errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used or expired")
			}
			orderInfo.CouponReserved = true
		}

		// Gift credit is reserved only after the row lock.  The fee has already
		// been calculated on the full external payable amount by design.
		if lockedUser.GiftAmount > 0 && orderInfo.Amount > 0 {
			orderInfo.GiftAmount = min(lockedUser.GiftAmount, orderInfo.Amount)
			orderInfo.Amount -= orderInfo.GiftAmount
		}
		if orderInfo.GiftAmount > 0 {
			lockedUser.GiftAmount -= orderInfo.GiftAmount
			if e := txStore.User().UpdateBalanceFields(l.ctx, lockedUser); e != nil {
				l.Errorw("[Purchase] Database update error", logger.Field("error", e.Error()), logger.Field("user", lockedUser))
				return e
			}
			// create deduction record
			giftLog := log.Gift{
				Type:        log.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     lockedUser.GiftAmount,
				Remark:      "Purchase order deduction",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if e := txStore.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: lockedUser.Id,
				Content:  string(content),
			}); e != nil {
				l.Errorw("[Purchase] Database insert error",
					logger.Field("error", e.Error()),
					logger.Field("deductionLog", giftLog),
				)
				return e
			}
		}

		reservedInventory, e := txStore.Subscribe().ReserveInventory(l.ctx, sub.Id)
		if e != nil {
			return e
		}
		if !reservedInventory {
			return errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
		}

		// insert order
		return txStore.Order().Insert(l.ctx, orderInfo)
	})
	if err != nil {
		l.Errorw("[Purchase] Database insert error", logger.Field("error", err.Error()), logger.Field("orderInfo", orderInfo))
		var codeErr *xerr.CodeError
		if errors.As(err, &codeErr) {
			return nil, err
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert order error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Errorw("[Purchase] Marshal payload error", logger.Field("error", err.Error()), logger.Field("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Errorw("[Purchase] Enqueue task error", logger.Field("error", err.Error()), logger.Field("task", task))
	} else {
		l.Infow("[Purchase] Enqueue task success", logger.Field("TaskID", taskInfo.ID))
	}

	return &dto.PurchaseOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
