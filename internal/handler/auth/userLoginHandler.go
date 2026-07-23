package auth

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/turnstile"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// UserLoginHandler documents User login.
//
// @Summary User login
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.UserLoginRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/login [post]
func UserLoginHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UserLoginRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = string(c.UserAgent())
		if svcCtx.Config.Verify.LoginVerify {
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
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := auth.NewUserLoginLogic(ctx, auth.UserLoginDependencies{
			Store: svcCtx.Store,
			Redis: svcCtx.Redis,
			Config: auth.UserLoginConfig{
				JWTAccessSecret: svcCtx.Config.JwtAuth.AccessSecret,
				JWTAccessExpire: svcCtx.Config.JwtAuth.AccessExpire,
			},
			Policy:       registerpolicy.NewServicePolicy(svcCtx),
			DeviceBinder: auth.NewBindDeviceLogic(ctx, auth.BindDeviceDependencies{Store: svcCtx.Store}),
		})
		resp, err := l.UserLogin(&req)
		result.HttpResult(c, resp, err)
	}
}
