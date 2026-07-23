package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// DeviceLoginHandler documents Device Login.
//
// @Summary Device Login
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.DeviceLoginRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/login/device [post]
func DeviceLoginHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.DeviceLoginRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		req.IP = c.ClientIP()

		l := auth.NewDeviceLoginLogic(ctx, auth.DeviceLoginDependencies{
			Store: svcCtx.Store,
			Redis: svcCtx.Redis,
			Config: auth.DeviceLoginConfig{
				Enabled:           svcCtx.Config.Device.Enable,
				OnlyRealDevice:    svcCtx.Config.Device.OnlyRealDevice,
				InviteForced:      svcCtx.Config.Invite.ForcedInvite,
				OnlyFirstPurchase: svcCtx.Config.Invite.OnlyFirstPurchase,
				TrialEnabled:      svcCtx.Config.Register.EnableTrial,
				TrialSubscribeID:  svcCtx.Config.Register.TrialSubscribe,
				TrialTime:         svcCtx.Config.Register.TrialTime,
				TrialTimeUnit:     svcCtx.Config.Register.TrialTimeUnit,
				JWTAccessSecret:   svcCtx.Config.JwtAuth.AccessSecret,
				JWTAccessExpire:   svcCtx.Config.JwtAuth.AccessExpire,
			},
			Policy: registerpolicy.NewServicePolicy(svcCtx),
		})
		resp, err := l.DeviceLogin(&req)
		result.HttpResult(c, resp, err)
	}
}
