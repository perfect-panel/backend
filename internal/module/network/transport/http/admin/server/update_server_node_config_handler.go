package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// ServerNodeConfigUpdater is the part of the network facade
// UpdateServerNodeConfigHandler calls.
type ServerNodeConfigUpdater interface {
	UpdateServerNodeConfig(ctx context.Context, req *dto.UpdateServerNodeConfigRequest) error
}

var _ ServerNodeConfigUpdater = network.Service(nil)

// UpdateServerNodeConfigHandler documents Update Server Node Config.
//
// @Summary Update Server Node Config
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateServerNodeConfigRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/server/node_config/update [post]
func UpdateServerNodeConfigHandler(service ServerNodeConfigUpdater) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.UpdateServerNodeConfigRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		err := service.UpdateServerNodeConfig(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
