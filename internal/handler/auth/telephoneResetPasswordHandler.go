package auth

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/turnstile"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Reset password
func TelephoneResetPasswordHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.TelephoneResetPasswordRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		if svcCtx.Config.Verify.ResetPasswordVerify {
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
		l := auth.NewTelephoneResetPasswordLogic(ctx, svcCtx)
		resp, err := l.TelephoneResetPassword(&req)
		result.HttpResult(c, resp, err)
	}
}
