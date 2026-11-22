package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CheckVerificationCodeHandler documents Check verification code.
//
// @Summary Check verification code
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.CheckVerificationCodeRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.CheckVerificationCodeResponse}
// @Router /v1/common/check_verification_code [post]
func CheckVerificationCodeHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.CheckVerificationCodeRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.CheckVerificationCode(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
