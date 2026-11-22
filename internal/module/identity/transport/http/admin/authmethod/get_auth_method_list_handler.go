// Package authmethod holds the identity module's admin handlers of the
// authentication methods: their configuration, the sender platforms and the
// test sends.
package authmethod

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetAuthMethodListResponse

// GetAuthMethodListHandler documents Get auth method list.
//
// @Summary Get auth method list
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetAuthMethodListResponse}
// @Router /v1/admin/auth-method/list [get]
func GetAuthMethodListHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetAuthMethodList(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
