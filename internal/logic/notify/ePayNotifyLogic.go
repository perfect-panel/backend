package notify

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	queueType "github.com/perfect-panel/server/queue/types"
)

type EPayNotifyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
	meta   EPayNotifyMeta
}

type EPayNotifyMeta struct {
	Method string
	Params map[string]string
}

// EPay notify
func NewEPayNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext, meta EPayNotifyMeta) *EPayNotifyLogic {
	if meta.Params == nil {
		meta.Params = make(map[string]string)
	}
	return &EPayNotifyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
		meta:   meta,
	}
}

func (l *EPayNotifyLogic) EPayNotify(req *types.EPayNotifyRequest) error {
	store := l.svcCtx.Store
	// Find payment config
	data, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		l.Logger.Error("[EPayNotify] Payment not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}

	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, req.OutTradeNo)
	if err != nil {
		l.Logger.Error("[EPayNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", req.OutTradeNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", req.OutTradeNo)
	}

	var config payment.EPayConfig
	if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
		l.Logger.Errorw("[EPayNotify] Unmarshal config failed", logger.Field("error", err.Error()))
		return err
	}

	client := epay.NewClient(config.Pid, config.Url, config.Key, config.Type)
	if !client.VerifySign(l.meta.Params) {
		if l.svcCtx.Config.Debug {
			l.Logger.Infow("[EPayNotify] Signature verification SKIPPED in debug mode",
				logger.Field("orderNo", req.OutTradeNo),
				logger.Field("method", l.meta.Method),
			)
		} else {
			l.Logger.Error("[EPayNotify] Verify sign failed",
				logger.Field("orderNo", req.OutTradeNo),
				logger.Field("receivedParams", l.meta.Params),
				logger.Field("method", l.meta.Method),
			)
			return errors.New("verify sign failed")
		}
	}

	if req.TradeStatus != "TRADE_SUCCESS" {
		l.Logger.Error("[EPayNotify] Trade status is not success", logger.Field("orderNo", req.OutTradeNo), logger.Field("tradeStatus", req.TradeStatus))
		return errors.New("trade status is not success")
	}
	if orderInfo.Status == 5 {
		return nil
	}
	// Update order status and trade_no
	err = store.Order().UpdateOrderStatusAndTradeNo(l.ctx, req.OutTradeNo, 2, req.TradeNo)
	if err != nil {
		l.Logger.Error("[EPayNotify] Update order status failed", logger.Field("error", err.Error()), logger.Field("orderNo", req.OutTradeNo))
		return err
	}
	// Create activate order task
	payload := queueType.ForthwithActivateOrderPayload{
		OrderNo: req.OutTradeNo,
	}
	bytes, err := json.Marshal(&payload)
	if err != nil {
		l.Logger.Error("[EPayNotify] Marshal payload failed", logger.Field("error", err.Error()))
		return err
	}
	task := asynq.NewTask(queueType.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
	taskInfo, err := l.svcCtx.Queue.EnqueueContext(l.ctx, task)
	if err != nil {
		l.Logger.Error("[EPayNotify] Enqueue task failed", logger.Field("error", err.Error()))
		return err
	}
	l.Logger.Info("[EPayNotify] Enqueue task success", logger.Field("taskInfo", taskInfo))
	return nil
}
