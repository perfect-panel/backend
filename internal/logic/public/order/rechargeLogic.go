package order

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
)

type RechargeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Recharge
func NewRechargeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RechargeLogic {
	return &RechargeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RechargeLogic) Recharge(req *dto.RechargeOrderRequest) (resp *dto.RechargeOrderResponse, err error) {
	store := l.svcCtx.Store
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// Validate recharge amount
	if req.Amount <= 0 {
		l.Errorw("[Recharge] Invalid recharge amount", logger.Field("amount", req.Amount), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "recharge amount must be greater than 0")
	}

	if req.Amount > MaxRechargeAmount {
		l.Errorw("[Recharge] Recharge amount exceeds maximum limit",
			logger.Field("amount", req.Amount),
			logger.Field("max", MaxRechargeAmount),
			logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "recharge amount exceeds maximum limit")
	}

	// find payment method
	payment, err := store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Errorw("[Recharge] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(err, "find payment error: %v", err.Error())
	}
	// Calculate the handling fee
	feeAmount := calculateFee(req.Amount, payment)
	totalAmount := req.Amount + feeAmount

	// Validate total amount after adding fee
	if totalAmount > MaxOrderAmount {
		l.Errorw("[Recharge] Total amount exceeds maximum limit after fee",
			logger.Field("amount", totalAmount),
			logger.Field("max", MaxOrderAmount),
			logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "total amount exceeds maximum limit")
	}

	// query user is new purchase or renewal
	isNew, err := store.Order().IsUserEligibleForNewOrder(l.ctx, u.Id)
	if err != nil {
		l.Errorw("[Recharge] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
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
	err = store.Order().Insert(l.ctx, &orderInfo)
	if err != nil {
		l.Errorw("[Recharge] Database insert error", logger.Field("error", err.Error()), logger.Field("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Errorw("[Recharge] Marshal payload error", logger.Field("error", err.Error()), logger.Field("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Errorw("[Recharge] Enqueue task error", logger.Field("error", err.Error()), logger.Field("task", task))
	} else {
		l.Infow("[Recharge] Enqueue task success", logger.Field("TaskID", taskInfo.ID))
	}
	return &dto.RechargeOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
