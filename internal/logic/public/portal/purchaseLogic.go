package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"

	"github.com/hibiken/asynq"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type PurchaseLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewPurchaseLogic Purchase subscription
func NewPurchaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PurchaseLogic {
	return &PurchaseLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

const (
	CloseOrderTimeMinutes = 15
)

func (l *PurchaseLogic) Purchase(req *types.PortalPurchaseRequest) (resp *types.PortalPurchaseResponse, err error) {
	// find user auth
	userAuth, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, req.AuthType, req.Identifier)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth error: %v", err.Error())
	}
	if userAuth.UserId != 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "user already exists")
	}
	// find subscribe plan
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.SubscribeId)
	if err != nil {
		l.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}

	// check subscribe plan stock
	if sub.Inventory == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
	}

	// check subscribe plan status
	if !*sub.Sell {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "subscribe not sell")
	}
	var discount float64 = 1
	if sub.Discount != "" {
		var dis []types.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	// discount amount
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	var couponAmount int64 = 0
	// Calculate the coupon deduction
	if req.Coupon != "" {
		couponInfo, err := l.svcCtx.Store.Coupon().FindOneByCode(l.ctx, req.Coupon)
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
		// Check expiration time
		expireTime := time.Unix(couponInfo.ExpireTime, 0)
		if timeutil.Now().After(expireTime) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponExpired), "coupon expired")
		}

		couponSub := tool.StringToInt64Slice(couponInfo.Subscribe)
		if len(couponSub) > 0 && !tool.Contains(couponSub, req.SubscribeId) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}

		couponAmount = calculateCoupon(amount, couponInfo)
	}
	// Calculate the handling fee
	amount -= couponAmount
	// find payment method
	paymentConfig, err := l.svcCtx.Store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Logger.Error("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "find payment method error: %v", err.Error())
	}

	if payment.ParsePlatform(paymentConfig.Platform) == payment.Balance {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "balance error")
	}

	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, paymentConfig)
	}
	// create order
	orderInfo := &order.Order{
		OrderNo:        tool.GenerateTradeNo(),
		Type:           1,
		Quantity:       req.Quantity,
		Price:          price,
		Amount:         amount,
		Discount:       discountAmount,
		GiftAmount:     0,
		Coupon:         req.Coupon,
		CouponDiscount: couponAmount,
		PaymentId:      req.Payment,
		Method:         paymentConfig.Platform,
		FeeAmount:      feeAmount,
		Status:         1,
		IsNew:          true,
		SubscribeId:    req.SubscribeId,
	}
	// save order
	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// save guest order and user information
		tempOrder := constant.TemporaryOrderInfo{
			OrderNo:    orderInfo.OrderNo,
			Identifier: req.Identifier,
			AuthType:   req.AuthType,
			Password:   req.Password,
			InviteCode: req.InviteCode,
		}
		content, _ := tempOrder.Marshal()

		if _, err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf(constant.TempOrderCacheKey, orderInfo.OrderNo), string(content), 24*time.Hour).Result(); err != nil {
			l.Errorw("[Purchase] Redis set error", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
			return err
		}
		l.Infow("[Purchase] Guest order", logger.Field("order_no", orderInfo.OrderNo), logger.Field("identifier", req.Identifier))

		// Decrease subscribe plan stock
		if sub.Inventory != -1 {
			sub.Inventory--
			if e := store.Subscribe().Update(l.ctx, sub); e != nil {
				l.Errorw("[Purchase] Database update error", logger.Field("error", e.Error()), logger.Field("subscribe_id", sub.Id))
				return e
			}
		}

		// save guest order
		if err = store.Order().Insert(l.ctx, orderInfo); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		l.Errorw("[Purchase] Database transaction error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "transaction error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Errorw("[CloseOrder Task] Marshal payload error", logger.Field("error", err.Error()), logger.Field("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Errorw("[CloseOrder Task] Enqueue task error", logger.Field("error", err.Error()), logger.Field("task", taskInfo))
	} else {
		l.Infow("[CloseOrder Task] Enqueue task success", logger.Field("TaskID", taskInfo.ID))
	}
	resp = &types.PortalPurchaseResponse{OrderNo: orderInfo.OrderNo}
	return resp, nil
}
