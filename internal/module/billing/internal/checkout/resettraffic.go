package checkout

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// ResetTraffic creates a paid traffic-reset order for an active subscription.
func (s *Service) ResetTraffic(ctx context.Context, req *dto.ResetTrafficOrderRequest) (*dto.ResetTrafficOrderResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// find user subscription
	userSubscribe, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, req.UserSubscribeID)
	if err != nil {
		log.Errorw("[ResetTraffic] Database query error", logger.Field("error", err.Error()), logger.Field("UserSubscribeID", req.UserSubscribeID))
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
		log.Errorw("[ResetTraffic] subscribe not found", logger.Field("UserSubscribeID", req.UserSubscribeID))
		return nil, errors.New("subscribe not found")
	}
	amount := userSubscribe.Subscribe.Replacement
	// find payment method
	payment, err := s.deps.Payments.FindOne(ctx, req.Payment)
	if err != nil {
		log.Errorw("[ResetTraffic] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
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
	orderflow.ApplyIdempotency(ctx, &orderInfo)
	// Billing-domain transaction: wallet deduction and order creation settle
	// together.
	err = s.deps.Store.InBillingTx(ctx, func(txStore repository.BillingStore) error {
		lockedUser, e := txStore.Wallet().FindOneForUpdate(ctx, u.Id)
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
			if err := txStore.Wallet().UpdateBalanceFields(ctx, lockedUser); err != nil {
				log.Errorw("[ResetTraffic] Database update error", logger.Field("error", err.Error()), logger.Field("user", lockedUser))
				return err
			}
			// create deduction record
			giftLog := logEntity.Gift{
				Type:        logEntity.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     lockedUser.GiftAmount,
				Remark:      "Renewal order deduction",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if err := txStore.Log().Insert(ctx, &logEntity.SystemLog{
				Type:     logEntity.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: lockedUser.Id,
				Content:  string(content),
			}); err != nil {
				log.Errorw("[ResetTraffic] Database insert error", logger.Field("error", err.Error()), logger.Field("deductionLog", content))
				return err
			}
		}
		// insert order
		return txStore.Order().Insert(ctx, &orderInfo)
	})
	if err != nil {
		log.Errorw("[ResetTraffic] Database insert error", logger.Field("error", err.Error()), logger.Field("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	s.enqueueDeferredClose(ctx, "[ResetTraffic]", orderInfo.OrderNo)
	return &dto.ResetTrafficOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
