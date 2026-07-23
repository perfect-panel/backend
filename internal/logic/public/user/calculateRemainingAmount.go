package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/logger"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/deduction"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func CalculateRemainingAmount(ctx context.Context, svcCtx *svc.ServiceContext, userSubscribeId int64) (int64, error) {
	// Find User Subscribe
	userSubscribe, err := svcCtx.Store.UserSubscription().FindOneUserSubscribe(ctx, userSubscribeId)
	if err != nil {
		logger.WithContext(ctx).Error("[func CalculateRemainingAmount(ctx context.Context, svcCtx *svc.ServiceContext, userSubscribeId int64) (int64, error) {\n] FindOneUserSubscribe", logger.Field("err", err.Error()), logger.Field("id", userSubscribeId))
		return 0, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed, id: %d", userSubscribeId)
	}
	if userSubscribe.OrderId == 0 {
		return 0, nil
	}
	if !*userSubscribe.Subscribe.AllowDeduction && !svcCtx.Config.Subscribe.SingleModel {
		return 0, errors.New("The subscription package does not support deductions")
	}

	if userSubscribe.Status != 1 {
		return 0, errors.New("The subscription package is not in use")
	}
	// Find Order Details
	orderDetails, err := svcCtx.Store.Order().FindOneDetails(ctx, userSubscribe.OrderId)
	if err != nil {
		logger.WithContext(ctx).Error("[PreUnsubscribe] FindOneDetails", logger.Field("err", err.Error()), logger.Field("id", userSubscribe.OrderId))
		return 0, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneDetails failed, id: %d", userSubscribe.OrderId)
	}
	// Calculate Order Quantity
	orderQuantity := orderDetails.Quantity
	// Calculate Order Amount
	orderAmount := orderDetails.Amount + orderDetails.GiftAmount

	if len(orderDetails.SubOrders) > 0 {
		for _, subOrder := range orderDetails.SubOrders {
			if subOrder.Status == 2 || subOrder.Status == 5 {
				orderAmount += subOrder.Amount + subOrder.GiftAmount
				orderQuantity += subOrder.Quantity
			}
		}
	}
	// Calculate Remaining Amount
	remainingAmount, err := deduction.CalculateRemainingAmount(
		deduction.Subscribe{
			StartTime:      userSubscribe.StartTime,
			ExpireTime:     userSubscribe.ExpireTime,
			Traffic:        userSubscribe.Traffic,
			Download:       userSubscribe.Download,
			Upload:         userSubscribe.Upload,
			UnitTime:       userSubscribe.Subscribe.UnitTime,
			UnitPrice:      userSubscribe.Subscribe.UnitPrice,
			ResetCycle:     userSubscribe.Subscribe.ResetCycle,
			DeductionRatio: userSubscribe.Subscribe.DeductionRatio,
		},
		deduction.Order{
			Amount:   orderAmount,
			Quantity: orderQuantity,
		},
	)
	if err != nil {
		return 0, errors.Wrapf(xerr.NewErrCode(500), "CalculateRemainingAmount failed, userSubscribeId: %d, err: %v", userSubscribeId, err)
	}
	return remainingAmount, nil
}
