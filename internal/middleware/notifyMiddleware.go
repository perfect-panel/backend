package middleware

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
)

type PaymentParams struct {
	Platform string `uri:"platform"`
	Token    string `uri:"token"`
}

func NotifyMiddleware(svc *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, requestCtx *app.RequestContext) {
		params := PaymentParams{
			Platform: requestCtx.Param("platform"),
			Token:    requestCtx.Param("token"),
		}
		ctx, err := PaymentNotifyContext(ctx, svc, params.Platform, params.Token)
		if err != nil {
			requestCtx.JSON(400, map[string]string{"error": err.Error()})
			requestCtx.Abort()
			return
		}
		requestCtx.Next(ctx)
	}
}

func PaymentNotifyContext(ctx context.Context, svc *svc.ServiceContext, platform, token string) (context.Context, error) {
	config, err := svc.Store.Payment().FindOneByPaymentToken(ctx, token)
	if err != nil {
		return ctx, err
	}
	if config.Platform != platform {
		return ctx, errors.New("payment callback platform mismatch")
	}
	ctx = context.WithValue(ctx, constant.CtxKeyPlatform, config.Platform)
	ctx = context.WithValue(ctx, constant.CtxKeyPayment, config)
	return ctx, nil
}
