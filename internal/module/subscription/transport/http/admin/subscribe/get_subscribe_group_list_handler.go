package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetSubscribeGroupListResponse

// GetSubscribeGroupListHandler documents Get subscribe group list.
//
// @Summary Get subscribe group list
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetSubscribeGroupListResponse}
// @Router /v1/admin/subscribe/group/list [get]
func GetSubscribeGroupListHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.GetSubscribeGroupList(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
