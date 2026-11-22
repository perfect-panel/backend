package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.QueryNodeTagResponse

// QueryNodeTagHandler documents Query all node tags.
//
// @Summary Query all node tags
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryNodeTagResponse}
// @Router /v1/admin/server/node/tags [get]
func QueryNodeTagHandler(service network.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QueryNodeTag(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
