package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Query all node tags
func QueryNodeTagHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := server.NewQueryNodeTagLogic(c, svcCtx)
		resp, err := l.QueryNodeTag()
		result.HttpResult(ctx, resp, err)
	}
}
