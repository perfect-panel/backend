package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetServerProtocolsHandler documents Get Server Protocols.
//
// @Summary Get Server Protocols
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetServerProtocolsRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetServerProtocolsResponse}
// @Router /v1/admin/server/protocols [get]
func GetServerProtocolsHandler(service network.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetServerProtocolsRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetServerProtocols(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
