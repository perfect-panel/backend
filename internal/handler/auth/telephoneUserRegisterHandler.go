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

// User Telephone register
func TelephoneUserRegisterHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req types.TelephoneRegisterRequest
		_ = httpx.ShouldBind(c, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = string(c.UserAgent())
		if svcCtx.Config.Verify.RegisterVerify {
			verifyTurns := turnstile.New(turnstile.Config{
				Secret:  svcCtx.Config.Verify.TurnstileSecret,
				Timeout: 3 * time.Second,
			})
			if verify, err := verifyTurns.Verify(ctx, req.CfToken, req.IP); err != nil || !verify {
				err = errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "error: %v, verify: %v", err, verify)
				result.HttpResult(c, nil, err)
				return
			}
		}
		l := auth.NewTelephoneUserRegisterLogic(ctx, svcCtx)
		resp, err := l.TelephoneUserRegister(&req)
		result.HttpResult(c, resp, err)
	}
}
