package notify

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/logic/notify"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/result"
)

const maxStripePayloadSize = 65_536

var errStripePayloadTooLarge = errors.New("http: request body too large")

// PaymentNotifyHandler Payment Notify
func PaymentNotifyHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		platform, ok := c.Value(constant.CtxKeyPlatform).(string)
		if !ok {
			logger.WithContext(c).Errorf("platform not found")
			result.HttpResult(ctx, nil, fmt.Errorf("platform not found"))
			return
		}

		switch payment.ParsePlatform(platform) {
		case payment.EPay, payment.CryptoSaaS:
			params, err := uniqueFormValues(nativeFormValues(ctx))
			if err != nil {
				logger.WithContext(c).Errorw("[PaymentNotifyHandler] ShouldBind failed", logger.Field("error", err.Error()))
				ctx.String(consts.StatusBadRequest, "invalid request")
				return
			}
			req := epayNotifyRequest(params)
			l := notify.NewEPayNotifyLogic(c, svcCtx, notify.EPayNotifyMeta{
				Method: string(ctx.Method()),
				Params: params,
			})
			if err := l.EPayNotify(req); err != nil {
				logger.WithContext(c).Errorf("EPayNotify failed: %v", err.Error())
				ctx.String(consts.StatusBadRequest, err.Error())
				return
			}
			ctx.String(consts.StatusOK, "success")
		case payment.Stripe:
			payload, err := stripePayload(ctx.Request.Body())
			if err != nil {
				result.HttpResult(ctx, nil, err)
				return
			}
			l := notify.NewStripeNotifyLogic(c, svcCtx)
			if err := l.StripeNotify(payload, string(ctx.GetHeader("Stripe-Signature"))); err != nil {
				result.HttpResult(ctx, nil, err)
				return
			}
			result.HttpResult(ctx, nil, nil)

		case payment.AlipayF2F:
			l := notify.NewAlipayNotifyLogic(c, svcCtx)
			if err := l.AlipayNotify(nativeFormValues(ctx)); err != nil {
				result.HttpResult(ctx, nil, err)
				return
			}
			// Return success to alipay
			ctx.String(consts.StatusOK, "success")

		default:
			logger.WithContext(c).Errorf("platform %s not support", platform)
		}
	}
}

func nativeFormValues(ctx *app.RequestContext) url.Values {
	values := make(url.Values)
	ctx.PostArgs().VisitAll(func(key, value []byte) {
		values.Add(string(key), string(value))
	})
	ctx.QueryArgs().VisitAll(func(key, value []byte) {
		values.Add(string(key), string(value))
	})
	return values
}

func stripePayload(payload []byte) ([]byte, error) {
	if len(payload) > maxStripePayloadSize {
		return nil, errStripePayloadTooLarge
	}
	return payload, nil
}

func uniqueFormValues(values url.Values) (map[string]string, error) {
	params := make(map[string]string, len(values))
	for key, value := range values {
		if len(value) != 1 {
			return nil, fmt.Errorf("callback parameter %q must occur exactly once", key)
		}
		params[key] = value[0]
	}
	return params, nil
}

func epayNotifyRequest(params map[string]string) *dto.EPayNotifyRequest {
	return &dto.EPayNotifyRequest{
		Pid:         params["pid"],
		TradeNo:     params["trade_no"],
		OutTradeNo:  params["out_trade_no"],
		Type:        params["type"],
		Name:        params["name"],
		Money:       params["money"],
		TradeStatus: params["trade_status"],
		Param:       params["param"],
		Sign:        params["sign"],
		SignType:    params["sign_type"],
	}
}
