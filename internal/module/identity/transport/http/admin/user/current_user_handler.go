package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.User

// CurrentUserService is the part of the identity facade CurrentUserHandler
// calls.
type CurrentUserService interface {
	CurrentUser(ctx context.Context) (*dto.User, error)
}

var _ CurrentUserService = identity.Service(nil)

// CurrentUserHandler documents Current user.
//
// @Summary Current user
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.User}
// @Router /v1/admin/user/current [get]
func CurrentUserHandler(service CurrentUserService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		resp, err := service.CurrentUser(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
