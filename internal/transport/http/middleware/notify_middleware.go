package middleware

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

type PaymentParams struct {
	Platform string `uri:"platform"`
	Token    string `uri:"token"`
}

// PaymentMethods finds the payment method whose notify URL carries a token.
// The billing facade provides it, so the middleware never reads billing's
// tables itself.
type PaymentMethods interface {
	FindPaymentMethodByToken(ctx context.Context, token string) (*payment.Payment, error)
}

// NotifyMiddleware resolves the payment method a gateway callback's URL names
// and puts it, with its platform, into the request context for the notify
// handler. A token that names no method, or a method of another platform
// than the URL's, is answered with 400 and the callback is not handled.
func NotifyMiddleware(methods PaymentMethods) app.HandlerFunc {
	return func(ctx context.Context, requestCtx *app.RequestContext) {
		params := PaymentParams{
			Platform: requestCtx.Param("platform"),
			Token:    requestCtx.Param("token"),
		}
		ctx, err := PaymentNotifyContext(ctx, methods, params.Platform, params.Token)
		if err != nil {
			requestCtx.JSON(400, map[string]string{"error": err.Error()})
			requestCtx.Abort()
			return
		}
		requestCtx.Next(ctx)
	}
}

// PaymentNotifyContext returns ctx carrying the payment method token names
// and its platform, refusing a method whose platform is not the URL's.
func PaymentNotifyContext(ctx context.Context, methods PaymentMethods, platform, token string) (context.Context, error) {
	config, err := methods.FindPaymentMethodByToken(ctx, token)
	if err != nil {
		return ctx, err
	}
	if config.Platform != platform {
		return ctx, errors.New("payment callback platform mismatch")
	}
	ctx = context.WithValue(ctx, requestctx.CtxKeyPlatform, config.Platform)
	ctx = context.WithValue(ctx, requestctx.CtxKeyPayment, config)
	return ctx, nil
}
