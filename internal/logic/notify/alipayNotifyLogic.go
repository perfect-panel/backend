package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/alipay"
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
	if client == nil {
		return errors.New("initialize Alipay client failed")
	}
	notify, err := client.DecodeNotification(form)
	if err != nil {
		l.Logger.Error("[AlipayNotify] Decode notification failed", logger.Field("error", err.Error()))
		return err
	}
	if notify.Status == alipay.Success || notify.Status == alipay.Finished {
		orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
		if err != nil {
			l.Logger.Error("[AlipayNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
			return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
		}

		if finished, err := validateAlipayCallback(orderInfo, data, &config, notify); err != nil {
			return err
		} else if finished {
			return nil
		}
		status, err := client.QueryTrade(l.ctx, notify.OrderNo)
		if err != nil {
			return err
		}
		if status != alipay.Success && status != alipay.Finished {
			return errors.New("Alipay trade is not paid")
		}
		if err := markOrderPaidAndEnqueue(l.ctx, l.svcCtx, orderInfo, notify.TradeNo); err != nil {
			return err
		}
		l.Logger.Info("[AlipayNotify] Notify status success", logger.Field("orderNo", notify.OrderNo))
	} else {
		l.Logger.Error("[AlipayNotify] Notify status failed", logger.Field("status", string(notify.Status)))
	}
	return nil
}

func validateAlipayCallback(orderInfo *order.Order, paymentConfig *payment.Payment, config *payment.AlipayF2FConfig, notify *alipay.Notification) (bool, error) {
	if notify == nil {
		return false, errors.New("Alipay callback is missing")
	}
	if err := validateOrderPayment(orderInfo, paymentConfig); err != nil {
		return false, err
	}
	if notify.AppId != config.AppId {
		return false, errors.New("Alipay app id mismatch")
	}
	if finished, err := finishedOrderDuplicate(orderInfo, notify.TradeNo); err != nil || finished {
		return finished, err
	}
	if err := validateOrderCanSettle(orderInfo); err != nil {
		return false, err
	}
	if err := validatePaymentExpectation(orderInfo, notify.Amount, "CNY"); err != nil {
		return false, err
	}
	return false, nil
}
