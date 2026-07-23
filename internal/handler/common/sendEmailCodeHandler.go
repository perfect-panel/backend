package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// SendEmailCodeHandler documents Get verification code.
//
// @Summary Get verification code
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.SendCodeRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.SendCodeResponse}
// @Router /v1/common/send_code [post]
func SendEmailCodeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.SendCodeRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := common.NewSendEmailCodeLogic(ctx, common.SendEmailCodeDependencies{
			Store: svcCtx.Store,
			Redis: svcCtx.Redis,
			Queue: svcCtx.Queue,
			Config: common.EmailCodeConfig{
				DomainSuffixList:   svcCtx.Config.Email.DomainSuffixList,
				EnableDomainSuffix: svcCtx.Config.Email.EnableDomainSuffix,
				VerifyCodeInterval: svcCtx.Config.VerifyCode.Interval,
				VerifyCodeLimit:    svcCtx.Config.VerifyCode.Limit,
				VerifyCodeExpire:   svcCtx.Config.VerifyCode.ExpireTime,
				SiteLogo:           svcCtx.Config.Site.SiteLogo,
				SiteName:           svcCtx.Config.Site.SiteName,
			},
			Policy: registerpolicy.NewServicePolicy(svcCtx),
		})
		resp, err := l.SendEmailCode(&req)
		result.HttpResult(c, resp, err)
	}
}
