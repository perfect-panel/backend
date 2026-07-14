package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// GetServerConfigHandler Get server config
func GetServerConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req := dto.GetServerConfigRequest{ServerCommon: commonReq}
		if validateErr := svcCtx.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		l := server.NewGetServerConfigLogic(c, svcCtx, server.RequestMeta{
			IfNoneMatch: string(ctx.GetHeader("If-None-Match")),
		})
		resp, err := l.GetServerConfig(&req)
		writeHeaders(ctx, l.ResponseMeta().Headers)
		if err != nil {
			if errors.Is(err, xerr.StatusNotModified) {
				ctx.String(consts.StatusNotModified, "Not Modified")
				return
			}
			ctx.String(consts.StatusNotFound, "Not Found")
			return
		}
		ctx.JSON(consts.StatusOK, resp)
	}
}
