package auth

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/turnstile"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// User Telephone login
func TelephoneLoginHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.TelephoneLoginRequest
		_ = httpx.ShouldBind(ctx, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}
		// get client ip
		req.IP = ctx.ClientIP()
		if svcCtx.Config.Verify.LoginVerify {
			verifyTurns := turnstile.New(turnstile.Config{
				Secret:  svcCtx.Config.Verify.TurnstileSecret,
				Timeout: 3 * time.Second,
			})
			if verify, err := verifyTurns.Verify(c, req.CfToken, req.IP); err != nil || !verify {
				err = errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "error: %v, verify: %v", err, verify)
				result.HttpResult(ctx, nil, err)
				return
			}
		}
		l := auth.NewTelephoneLoginLogic(c, svcCtx)
		resp, err := l.TelephoneLogin(&req, ctx.ClientIP(), string(ctx.UserAgent()))
		result.HttpResult(ctx, resp, err)
	}
}
