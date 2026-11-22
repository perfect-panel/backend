package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetDeviceListResponse

// GetDeviceListService is the part of the identity facade
// GetDeviceListHandler calls.
type GetDeviceListService interface {
	GetDeviceList(ctx context.Context) (*dto.GetDeviceListResponse, error)
}

var _ GetDeviceListService = identity.Service(nil)

// GetDeviceListHandler documents Get Device List.
//
// @Summary Get Device List
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetDeviceListResponse}
// @Router /v1/public/user/devices [get]
func GetDeviceListHandler(service GetDeviceListService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.GetDeviceList(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
