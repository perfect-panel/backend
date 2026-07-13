package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

func GetServerNodeConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.GetServerNodeConfigRequest
		_ = httpx.ShouldBind(ctx, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		l := server.NewGetServerNodeConfigLogic(c, svcCtx)
		resp, err := l.GetServerNodeConfig(&req)
		result.HttpResult(ctx, resp, err)
	}
}
