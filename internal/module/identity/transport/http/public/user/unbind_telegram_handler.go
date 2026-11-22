package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UnbindTelegramService is the part of the identity facade
// UnbindTelegramHandler calls.
type UnbindTelegramService interface {
	UnbindTelegram(ctx context.Context) error
}

var _ UnbindTelegramService = identity.Service(nil)

// UnbindTelegramHandler documents Unbind Telegram.
//
// @Summary Unbind Telegram
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/public/user/unbind_telegram [post]
func UnbindTelegramHandler(service UnbindTelegramService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		err := service.UnbindTelegram(c)
		httpx.HttpResult(ctx, nil, err)
	}
}
