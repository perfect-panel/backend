// Package common holds the identity module's handlers of the verification
// codes: sending email and SMS codes and pre-checking them.
package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// SendEmailCodeHandler documents Get verification code.
//
// @Summary Get verification code
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.SendCodeRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.SendCodeResponse}
// @Router /v1/common/send_code [post]
func SendEmailCodeHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.SendCodeRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.SendEmailCode(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
