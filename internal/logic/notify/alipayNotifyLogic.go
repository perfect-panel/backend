package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/alipay"
	"github.com/perfect-panel/server/queue/types"
)

type AlipayNotifyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Alipay notify
func NewAlipayNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AlipayNotifyLogic {
	return &AlipayNotifyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AlipayNotifyLogic) AlipayNotify(form url.Values) error {
	store := l.svcCtx.Store
	data, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		return fmt.Errorf("payment config not found")
	}
	var config payment.AlipayF2FConfig
	if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
		l.Logger.Error("[AlipayNotify] Unmarshal config failed", logger.Field("error", err.Error()))
		return err
	}
	client := alipay.NewClient(alipay.Config{
		AppId:       config.AppId,
		PrivateKey:  config.PrivateKey,
		PublicKey:   config.PublicKey,
		InvoiceName: config.InvoiceName,
		NotifyURL:   data.Domain + "/v1/payment/alipay/notify",
		Sandbox:     config.Sandbox,
	})
	notify, err := client.DecodeNotification(form)
	if err != nil {
		l.Logger.Error("[AlipayNotify] Decode notification failed", logger.Field("error", err.Error()))
		return err
	}
	if notify.Status == alipay.Success {
		orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
		if err != nil {
			l.Logger.Error("[AlipayNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
			return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
		}

		if orderInfo.Status == 5 {
			return nil
		}

		// Update order status and trade_no
		err = store.Order().UpdateOrderStatusAndTradeNo(l.ctx, notify.OrderNo, 2, notify.TradeNo)
		if err != nil {
			l.Logger.Error("[AlipayNotify] Update order status failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
			return err
		}
		l.Logger.Info("[AlipayNotify] Notify status success", logger.Field("orderNo", notify.OrderNo))
		payload := types.ForthwithActivateOrderPayload{
			OrderNo: notify.OrderNo,
		}
		bytes, err := json.Marshal(&payload)
		if err != nil {
			l.Logger.Error("[AlipayNotify] Marshal payload failed", logger.Field("error", err.Error()))
			return err
		}
		task := asynq.NewTask(types.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
		taskInfo, err := l.svcCtx.Queue.EnqueueContext(l.ctx, task)
		if err != nil {
			l.Logger.Error("[AlipayNotify] Enqueue task failed", logger.Field("error", err.Error()))
			return err
		}
		l.Logger.Info("[AlipayNotify] Enqueue task success", logger.Field("taskInfo", taskInfo))
	} else {
		l.Logger.Error("[AlipayNotify] Notify status failed", logger.Field("status", string(notify.Status)))
	}
	return nil
}
