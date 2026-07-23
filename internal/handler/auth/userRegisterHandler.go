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

// UserRegisterHandler documents registers a user..
//
// @Summary registers a user.
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.UserRegisterRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/register [post]
func UserRegisterHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UserRegisterRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = string(c.UserAgent())
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := auth.NewUserRegisterLogic(ctx, auth.UserRegisterDependencies{
			Store: svcCtx.Store,
			Redis: svcCtx.Redis,
			Config: auth.UserRegisterConfig{
				EmailDomainSuffixList:   svcCtx.Config.Email.DomainSuffixList,
				EmailEnableDomainSuffix: svcCtx.Config.Email.EnableDomainSuffix,
				EmailVerifyEnabled:      svcCtx.Config.Email.EnableVerify,
				InviteForced:            svcCtx.Config.Invite.ForcedInvite,
				OnlyFirstPurchase:       svcCtx.Config.Invite.OnlyFirstPurchase,
				TrialEnabled:            svcCtx.Config.Register.EnableTrial,
				TrialSubscribeID:        svcCtx.Config.Register.TrialSubscribe,
				TrialTime:               svcCtx.Config.Register.TrialTime,
				TrialTimeUnit:           svcCtx.Config.Register.TrialTimeUnit,
				JWTAccessSecret:         svcCtx.Config.JwtAuth.AccessSecret,
				JWTAccessExpire:         svcCtx.Config.JwtAuth.AccessExpire,
			},
			Policy:       registerpolicy.NewServicePolicy(svcCtx),
			DeviceBinder: auth.NewBindDeviceLogic(ctx, auth.BindDeviceDependencies{Store: svcCtx.Store}),
		})
		resp, err := l.UserRegister(&req)
		result.HttpResult(c, resp, err)
	}
}
