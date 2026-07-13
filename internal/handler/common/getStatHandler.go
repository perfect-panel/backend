package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get stat
func GetStatHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := common.NewGetStatLogic(ctx, svcCtx)
		resp, err := l.GetStat()
		result.HttpResult(c, resp, err)
	}
}
