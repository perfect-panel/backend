package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// setting telegram bot
func SettingTelegramBotHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := system.NewSettingTelegramBotLogic(ctx, svcCtx)
		err := l.SettingTelegramBot()
		result.HttpResult(c, nil, err)
	}
}
