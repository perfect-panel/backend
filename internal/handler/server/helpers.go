package server

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

func ServerMiddleware(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		key, ok := ctx.GetQuery("secret_key")
		if ok && key == svcCtx.Config.Node.NodeSecret {
			ctx.Next(c)
			return
		}
		ctx.String(consts.StatusForbidden, "Forbidden")
		ctx.Abort()
	}
}

func serverCommonRequest(ctx *app.RequestContext) (dto.ServerCommon, error) {
	var serverID int64
	if rawServerID := ctx.Query("server_id"); rawServerID != "" {
		id, err := strconv.ParseInt(rawServerID, 10, 64)
		if err != nil {
			return dto.ServerCommon{}, err
		}
		serverID = id
	}
	return dto.ServerCommon{
		Protocol:  ctx.Query("protocol"),
		ServerId:  serverID,
		SecretKey: ctx.Query("secret_key"),
	}, nil
}

func queryValues(ctx *app.RequestContext, keys ...string) []string {
	var values []string
	for _, key := range keys {
		for _, value := range ctx.QueryArgs().PeekAll(key) {
			values = append(values, string(value))
		}
	}
	return values
}

func writeHeaders(ctx *app.RequestContext, headers map[string]string) {
	for key, value := range headers {
		ctx.Header(key, value)
	}
}

func writeHTTPResult(ctx *app.RequestContext, resp interface{}, err error) {
	res := result.BuildHTTPResult(resp, err)
	ctx.JSON(res.StatusCode, res.Body)
}

func writeParamError(ctx *app.RequestContext, err error) {
	resp := result.BuildParamErrorResult(err)
	ctx.JSON(resp.StatusCode, resp.Body)
}
