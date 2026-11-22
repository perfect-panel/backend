package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// LogoutService is the part of the identity facade LogoutHandler calls.
type LogoutService interface {
	Logout(ctx context.Context) error
}

var _ LogoutService = identity.Service(nil)

// LogoutHandler documents Logout.
//
// @Summary Logout
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/public/user/logout [post]
func LogoutHandler(service LogoutService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		err := service.Logout(c)
		httpx.HttpResult(ctx, nil, err)
	}
}
