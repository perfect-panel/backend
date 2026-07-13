package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Heartbeat
func HeartbeatHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := common.NewHeartbeatLogic(ctx, svcCtx)
		resp, err := l.Heartbeat()
		result.HttpResult(c, resp, err)
	}
}
