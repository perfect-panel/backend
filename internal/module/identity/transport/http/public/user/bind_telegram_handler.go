package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.BindTelegramResponse

// BindTelegramService is the part of the identity facade BindTelegramHandler
// calls.
type BindTelegramService interface {
	BindTelegram(ctx context.Context) (*dto.BindTelegramResponse, error)
}

var _ BindTelegramService = identity.Service(nil)

// BindTelegramHandler documents Bind Telegram.
//
// @Summary Bind Telegram
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.BindTelegramResponse}
// @Router /v1/public/user/bind_telegram [get]
func BindTelegramHandler(service BindTelegramService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.BindTelegram(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
