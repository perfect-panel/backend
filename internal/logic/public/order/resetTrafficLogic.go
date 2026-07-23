package order

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
)

type ResetTrafficLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Reset traffic
func NewResetTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetTrafficLogic {
	return &ResetTrafficLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetTrafficLogic) ResetTraffic(req *dto.ResetTrafficOrderRequest) (resp *dto.ResetTrafficOrderResponse, err error) {
	store := l.svcCtx.Store
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// find user subscription
	userSubscribe, err := store.UserSubscription().FindOneUserSubscribe(l.ctx, req.UserSubscribeID)
	if err != nil {
		l.Errorw("[ResetTraffic] Database query error", logger.Field("error", err.Error()), logger.Field("UserSubscribeID", req.UserSubscribeID))
		return nil, errors.Wrapf(err, "find user subscribe error: %v", err.Error())
	}
	if userSubscribe.UserId != u.Id {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "subscription does not belong to the current user")
	}
	// NoLimit subscriptions use the Unix epoch as their expiry sentinel. A paid
	// traffic reset must not be created for a subscription whose finite term has
	// already elapsed, because it cannot restore access or extend that term.
	now := timeutil.Now()
	if userSubscribe.ExpireTime.Unix() > 0 && userSubscribe.ExpireTime.Before(now) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscription expired")
	}
	if userSubscribe.Subscribe == nil {
		l.Errorw("[ResetTraffic] subscribe not found", logger.Field("UserSubscribeID", req.UserSubscribeID))
		return nil, errors.New("subscribe not found")
	}
	amount := userSubscribe.Subscribe.Replacement
	// find payment method
	payment, err := store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Errorw("[ResetTraffic] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(err, "find payment error: %v", err.Error())
	}
	if err := ensurePaymentAvailable(payment); err != nil {
		return nil, err
	}
	// create order
	orderInfo := order.Order{
		Id:             0,
		ParentId:       userSubscribe.OrderId,
		UserId:         u.Id,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           3,
		Price:          userSubscribe.Subscribe.Replacement,
		Amount:         amount,
		GiftAmount:     0,
		FeeAmount:      0,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		Status:         1,
		SubscribeId:    userSubscribe.SubscribeId,
		SubscribeToken: userSubscribe.Token,
	}
	// Database transaction
	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		lockedUser, e := txStore.User().FindOneForUpdate(l.ctx, u.Id)
		if e != nil {
			return e
		}
		if lockedUser.GiftAmount > 0 && orderInfo.Amount > 0 {
			orderInfo.GiftAmount = min(lockedUser.GiftAmount, orderInfo.Amount)
			orderInfo.Amount -= orderInfo.GiftAmount
		}
		if orderInfo.Amount > 0 {
			orderInfo.FeeAmount = calculateFee(orderInfo.Amount, payment)
			orderInfo.Amount += orderInfo.FeeAmount
		}
		if orderInfo.Amount > MaxOrderAmount {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
		}

		if orderInfo.GiftAmount > 0 {
			lockedUser.GiftAmount -= orderInfo.GiftAmount
			if err := txStore.User().UpdateBalanceFields(l.ctx, lockedUser); err != nil {
				l.Errorw("[ResetTraffic] Database update error", logger.Field("error", err.Error()), logger.Field("user", lockedUser))
				return err
			}
			// create deduction record
			giftLog := log.Gift{
				Type:        log.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     lockedUser.GiftAmount,
				Remark:      "Renewal order deduction",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if err = txStore.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: lockedUser.Id,
				Content:  string(content),
			}); err != nil {
				l.Errorw("[ResetTraffic] Database insert error", logger.Field("error", err.Error()), logger.Field("deductionLog", content))
				return err
			}
		}
		// insert order
		return txStore.Order().Insert(l.ctx, &orderInfo)
	})
	if err != nil {
		l.Errorw("[ResetTraffic] Database insert error", logger.Field("error", err.Error()), logger.Field("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Errorw("[ResetTraffic] Marshal payload error", logger.Field("error", err.Error()), logger.Field("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Errorw("[ResetTraffic] Enqueue task error", logger.Field("error", err.Error()), logger.Field("task", task))
	} else {
		l.Infow("[ResetTraffic] Enqueue task success", logger.Field("TaskID", taskInfo.ID))
	}
	return &dto.ResetTrafficOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
