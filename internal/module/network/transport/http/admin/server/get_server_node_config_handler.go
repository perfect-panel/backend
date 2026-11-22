package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetServerNodeConfigHandler documents Get Server Node Config.
//
// @Summary Get Server Node Config
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetServerNodeConfigRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetServerNodeConfigResponse}
// @Router /v1/admin/server/node_config [get]
func GetServerNodeConfigHandler(service network.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetServerNodeConfigRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetServerNodeConfig(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
