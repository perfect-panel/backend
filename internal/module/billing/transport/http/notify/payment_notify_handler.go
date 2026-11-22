// Package notify holds the HTTP handler that receives payment gateway
// callbacks and hands them to the billing facade to authenticate and settle.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
)

// maxNotifyPayloadSize caps the raw body of gateways that sign it.
const maxNotifyPayloadSize = 65_536

var errNotifyPayloadTooLarge = errors.New("http: request body too large")

// PaymentNotifyHandler documents Payment Notify.
//
// @Summary Payment Notify
// @Tags common
// @Accept json,x-www-form-urlencoded
// @Produce json
// @Param platform path string true "platform"
// @Param token path string true "token"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/notify/{platform}/{token} [delete]
// @Router /v1/notify/{platform}/{token} [get]
// @Router /v1/notify/{platform}/{token} [head]
// @Router /v1/notify/{platform}/{token} [options]
// @Router /v1/notify/{platform}/{token} [patch]
// @Router /v1/notify/{platform}/{token} [post]
// @Router /v1/notify/{platform}/{token} [put]
func PaymentNotifyHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		platform, ok := c.Value(requestctx.CtxKeyPlatform).(string)
		if !ok {
			logger.WithContext(c).Errorf("platform not found")
			httpx.HttpResult(ctx, nil, fmt.Errorf("platform not found"))
			return
		}
		style, ok := service.PaymentCallbackStyle(platform)
		if !ok {
			logger.WithContext(c).Errorf("platform %s not support", platform)
			ctx.String(consts.StatusBadRequest, "unsupported payment platform")
			return
		}
		notification := billing.PaymentNotification{HTTPMethod: string(ctx.Method())}
		if style.Body {
			payload, err := notifyPayload(ctx.Request.Body())
			if err != nil {
				if style.StatusFailure {
					failWithStatus(ctx, fmt.Errorf("%w: %w", billing.ErrInvalidPaymentCallback, err))
					return
				}
				httpx.HttpResult(ctx, nil, err)
				return
			}
			notification.Body = payload
			notification.Signature = string(ctx.GetHeader("Stripe-Signature"))
		} else {
			notification.Form = nativeFormValues(ctx)
			if style.UniqueParams {
				params, err := uniqueFormValues(notification.Form)
				if err != nil {
					logger.WithContext(c).Errorw("[PaymentNotifyHandler] ShouldBind failed", logger.Field("error", err.Error()))
					ctx.String(consts.StatusBadRequest, "invalid request")
					return
				}
				notification.Params = params
			}
		}
		if err := service.PaymentNotify(c, notification); err != nil {
			switch {
			case style.TextFailure:
				ctx.String(consts.StatusBadRequest, err.Error())
			case style.StatusFailure:
				failWithStatus(ctx, err)
			default:
				httpx.HttpResult(ctx, nil, err)
			}
			return
		}
		if style.TextReply {
			ctx.String(consts.StatusOK, "success")
			return
		}
		httpx.HttpResult(ctx, nil, nil)
	}
}

// failWithStatus answers a rejected callback of a gateway that reads the
// HTTP status: 400 when the callback cannot be authenticated or does not
// describe its order, so the gateway stops redelivering it, and 500 for a
// processing failure, so the gateway redelivers it. Every other route
// answers HTTP 200 with the outcome in the body, which such a gateway would
// take as success and never retry. The body keeps the error envelope.
func failWithStatus(ctx *app.RequestContext, err error) {
	status := consts.StatusInternalServerError
	if errors.Is(err, billing.ErrInvalidPaymentCallback) {
		status = consts.StatusBadRequest
	}
	ctx.JSON(status, httpx.BuildHTTPResult(nil, err).Body)
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

func notifyPayload(payload []byte) ([]byte, error) {
	if len(payload) > maxNotifyPayloadSize {
		return nil, errNotifyPayloadTooLarge
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
