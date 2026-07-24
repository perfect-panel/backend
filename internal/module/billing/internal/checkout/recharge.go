package checkout

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Recharge creates a balance recharge order.
func (s *Service) Recharge(ctx context.Context, req *dto.RechargeOrderRequest) (*dto.RechargeOrderResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// Validate recharge amount
	if req.Amount < MinRechargeAmount {
		log.Errorw("[Recharge] Invalid recharge amount", logger.Field("amount", req.Amount), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "recharge amount must be at least %d", MinRechargeAmount)
	}

	if req.Amount > MaxRechargeAmount {
		log.Errorw("[Recharge] Recharge amount exceeds maximum limit",
			logger.Field("amount", req.Amount),
			logger.Field("max", MaxRechargeAmount),
			logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "recharge amount exceeds maximum limit")
	}

	// find payment method
	payment, err := s.deps.Payments.FindOne(ctx, req.Payment)
	if err != nil {
		log.Errorw("[Recharge] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(err, "find payment error: %v", err.Error())
	}
	if err := ensurePaymentAvailable(payment); err != nil {
		return nil, err
	}
	// Calculate the handling fee
	feeAmount := calculateFee(req.Amount, payment)
	totalAmount := req.Amount + feeAmount

	// Validate total amount after adding fee
	if totalAmount > MaxOrderAmount {
		log.Errorw("[Recharge] Total amount exceeds maximum limit after fee",
			logger.Field("amount", totalAmount),
			logger.Field("max", MaxOrderAmount),
			logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "total amount exceeds maximum limit")
	}

	// query user is new purchase or renewal
	isNew, err := s.deps.Orders.IsUserEligibleForNewOrder(ctx, u.Id)
	if err != nil {
		log.Errorw("[Recharge] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(err, "query user error: %v", err.Error())
	}
	orderInfo := order.Order{
		UserId:    u.Id,
		OrderNo:   tool.GenerateTradeNo(),
		Type:      4,
		Price:     req.Amount,
		Amount:    totalAmount,
		FeeAmount: feeAmount,
		PaymentId: payment.Id,
		Method:    payment.Platform,
		Status:    1,
		IsNew:     isNew,
	}
	orderflow.ApplyIdempotency(ctx, &orderInfo)
	if err := s.deps.Orders.Insert(ctx, &orderInfo); err != nil {
		log.Errorw("[Recharge] Database insert error", logger.Field("error", err.Error()), logger.Field("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	s.enqueueDeferredClose(ctx, "[Recharge]", orderInfo.OrderNo)
	return &dto.RechargeOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
