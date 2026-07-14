package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
)

// ServerPushUserTrafficHandler Push user Traffic
func ServerPushUserTrafficHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := dto.ServerPushUserTrafficRequest{}
		_ = ctx.BindJSON(&req)
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req.ServerCommon = commonReq
		if validateErr := svcCtx.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		l := server.NewServerPushUserTrafficLogic(c, svcCtx)
		writeHTTPResult(ctx, nil, l.ServerPushUserTraffic(&req))
	}
}
