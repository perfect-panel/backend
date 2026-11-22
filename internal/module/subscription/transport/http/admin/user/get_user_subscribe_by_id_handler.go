package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetUserSubscribeByIdHandler documents Get user subcribe by id.
//
// @Summary Get user subcribe by id
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetUserSubscribeByIdRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.UserSubscribeDetail}
// @Router /v1/admin/user/subscribe/detail [get]
func GetUserSubscribeByIdHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetUserSubscribeByIdRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetUserSubscribeById(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
