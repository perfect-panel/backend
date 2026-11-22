// Package user holds the identity module's self-service handlers of the
// signed-in account: its profile, credentials, bindings, devices and
// notification settings.
package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.User

// QueryUserInfoService is the part of the identity facade
// QueryUserInfoHandler calls.
type QueryUserInfoService interface {
	QueryUserInfo(ctx context.Context) (*dto.User, error)
}

var _ QueryUserInfoService = identity.Service(nil)

// QueryUserInfoHandler documents returns the current user profile..
//
// @Summary returns the current user profile.
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.User}
// @Router /v1/public/user/info [get]
func QueryUserInfoHandler(service QueryUserInfoService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QueryUserInfo(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
