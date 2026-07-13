package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get log setting
func GetLogSettingHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := log.NewGetLogSettingLogic(ctx, svcCtx)
		resp, err := l.GetLogSetting()
		result.HttpResult(c, resp, err)
	}
}
