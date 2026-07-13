package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get Client
func GetClientHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		l := common.NewGetClientLogic(ctx, svcCtx)
		resp, err := l.GetClient()
		result.HttpResult(c, resp, err)
	}
}
