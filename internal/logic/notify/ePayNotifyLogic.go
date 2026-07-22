package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
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

func (l *EPayNotifyLogic) EPayNotify(req *dto.EPayNotifyRequest) error {
	if req == nil {
		return errors.New("callback request is missing")
	}
	store := l.svcCtx.Store
	data, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		l.Logger.Error("[EPayNotify] Payment not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}

	credentials, err := epayCredentialsForPayment(data)
	if err != nil {
		l.Logger.Errorw("[EPayNotify] Unmarshal config failed", logger.Field("error", err.Error()))
		return err
	}
	client := epay.NewClient(credentials.merchantID, credentials.endpoint, credentials.key, credentials.paymentType)
	if !client.VerifySign(l.meta.Params) {
		l.Logger.Error("[EPayNotify] Verify sign failed",
			logger.Field("orderNo", req.OutTradeNo),
			logger.Field("method", l.meta.Method),
		)
		return errors.New("verify sign failed")
	}
	callbackAmount, err := validateEPayCallback(req, l.meta.Params, credentials)
	if err != nil {
		l.Logger.Error("[EPayNotify] Callback validation failed", logger.Field("orderNo", req.OutTradeNo), logger.Field("error", err.Error()))
		return err
	}

	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, req.OutTradeNo)
	if err != nil {
		l.Logger.Error("[EPayNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", req.OutTradeNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", req.OutTradeNo)
	}
	if err := validateOrderPayment(orderInfo, data); err != nil {
		l.Logger.Error("[EPayNotify] Order payment binding failed", logger.Field("orderNo", req.OutTradeNo), logger.Field("error", err.Error()))
		return err
	}
	if finished, err := finishedOrderDuplicate(l.ctx, orderInfo, req.TradeNo); err != nil {
		return err
	} else if finished {
		return nil
	}
	if err := validateOrderCanSettle(orderInfo); err != nil {
		return err
	}
	if err := validatePaymentExpectation(orderInfo, callbackAmount, "CNY"); err != nil {
		l.Logger.Error("[EPayNotify] Payment amount validation failed", logger.Field("orderNo", req.OutTradeNo), logger.Field("error", err.Error()))
		return err
	}

	queried, err := client.QueryOrder(req.OutTradeNo)
	if err != nil {
		if errors.Is(err, epay.ErrQueryNotSupported) {
			// This gateway does not implement the order query API.
			// The callback signature was already verified above, so it is safe
			// to proceed without the active confirmation step.
			l.Logger.Infow("[EPayNotify] Gateway does not support order query; accepting signature-verified callback",
				logger.Field("orderNo", req.OutTradeNo),
			)
		} else {
			l.Logger.Error("[EPayNotify] Gateway order query failed", logger.Field("orderNo", req.OutTradeNo), logger.Field("error", err.Error()))
			return err
		}
	} else {
		if err := validateQueriedEPayOrder(queried, req, credentials, callbackAmount); err != nil {
			l.Logger.Error("[EPayNotify] Gateway order validation failed", logger.Field("orderNo", req.OutTradeNo), logger.Field("error", err.Error()))
			return err
		}
	}

	if err := markOrderPaidAndEnqueue(l.ctx, l.svcCtx, orderInfo, req.TradeNo); err != nil {
		l.Logger.Error("[EPayNotify] Settle order failed", logger.Field("orderNo", req.OutTradeNo), logger.Field("error", err.Error()))
		return err
	}
	l.Logger.Info("[EPayNotify] Notify processed", logger.Field("orderNo", req.OutTradeNo))
	return nil
}

type epayCredentials struct {
	merchantID  string
	endpoint    string
	key         string
	paymentType string
}

func epayCredentialsForPayment(data *payment.Payment) (epayCredentials, error) {
	var result epayCredentials
	switch paymentPlatform.ParsePlatform(data.Platform) {
	case paymentPlatform.EPay:
		var config payment.EPayConfig
		if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
			return result, err
		}
		result = epayCredentials{
			merchantID:  config.Pid,
			endpoint:    config.Url,
			key:         config.Key,
			paymentType: config.Type,
		}
	case paymentPlatform.CryptoSaaS:
		var config payment.CryptoSaaSConfig
		if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
			return result, err
		}
		result = epayCredentials{
			merchantID:  config.AccountID,
			endpoint:    config.Endpoint,
			key:         config.SecretKey,
			paymentType: config.Type,
		}
	default:
		return result, errors.New("unsupported EPay callback platform")
	}
	if result.merchantID == "" || result.endpoint == "" || result.key == "" {
		return result, errors.New("incomplete payment configuration")
	}
	return result, nil
}

func validateEPayCallback(req *dto.EPayNotifyRequest, params map[string]string, credentials epayCredentials) (int64, error) {
	if req == nil {
		return 0, errors.New("callback request is missing")
	}
	fields := map[string]string{
		"pid":          req.Pid,
		"trade_no":     req.TradeNo,
		"out_trade_no": req.OutTradeNo,
		"type":         req.Type,
		"name":         req.Name,
		"money":        req.Money,
		"trade_status": req.TradeStatus,
		"param":        req.Param,
		"sign":         req.Sign,
		"sign_type":    req.SignType,
	}
	for name, value := range fields {
		if params[name] != value {
			return 0, fmt.Errorf("callback parameter mismatch: %s", name)
		}
	}
	if req.OutTradeNo == "" || len(req.OutTradeNo) > 255 || strings.TrimSpace(req.OutTradeNo) != req.OutTradeNo {
		return 0, errors.New("invalid order number")
	}
	if err := validateTradeNo(req.TradeNo); err != nil {
		return 0, err
	}
	if req.Pid != credentials.merchantID {
		return 0, errors.New("merchant id mismatch")
	}
	if credentials.paymentType != "" && req.Type != credentials.paymentType {
		return 0, errors.New("payment type mismatch")
	}
	if req.TradeStatus != "TRADE_SUCCESS" {
		return 0, errors.New("trade status is not success")
	}
	if !strings.EqualFold(req.SignType, "MD5") {
		return 0, errors.New("unsupported signature type")
	}
	amount, err := epay.ParseMoney(req.Money)
	if err != nil {
		return 0, errors.New("invalid callback money")
	}
	return amount, nil
}

func validateQueriedEPayOrder(result *epay.QueryResult, req *dto.EPayNotifyRequest, credentials epayCredentials, callbackAmount int64) error {
	if result == nil || !result.Paid {
		return errors.New("gateway order is not paid")
	}
	if result.OrderNo != req.OutTradeNo {
		return errors.New("gateway order number mismatch")
	}
	if result.TradeNo == "" || result.TradeNo != req.TradeNo {
		return errors.New("gateway trade number mismatch")
	}
	if result.MerchantID != credentials.merchantID {
		return errors.New("gateway merchant id mismatch")
	}
	if result.Type != req.Type || (credentials.paymentType != "" && result.Type != credentials.paymentType) {
		return errors.New("gateway payment type mismatch")
	}
	queriedAmount, err := epay.ParseMoney(result.Money)
	if err != nil || queriedAmount != callbackAmount {
		return errors.New("gateway payment amount mismatch")
	}
	return nil
}
