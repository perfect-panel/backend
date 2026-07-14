package server

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

// QueryServerProtocolConfigHandler Get Server Protocol Config
func QueryServerProtocolConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		serverID, err := strconv.ParseInt(ctx.Param("server_id"), 10, 64)
		if err != nil {
			logger.WithContext(c).Debugf("[QueryServerProtocolConfigHandler] Parse server_id error: %v, Param: %s", err, ctx.Param("server_id"))
			ctx.String(consts.StatusBadRequest, "Invalid Params")
			ctx.Abort()
			return
		}
		req := dto.QueryServerConfigRequest{
			ServerID:  serverID,
			SecretKey: ctx.Query("secret_key"),
			Protocols: queryValues(ctx, "protocols", "protocols[]"),
		}
		if svcCtx.Config.Node.NodeSecret != req.SecretKey {
			ctx.String(consts.StatusUnauthorized, "Unauthorized")
			ctx.Abort()
			return
		}

		l := server.NewQueryServerProtocolConfigLogic(c, svcCtx)
		resp, err := l.QueryServerProtocolConfig(&req)
		if err != nil {
			writeHTTPResult(ctx, nil, err)
			return
		}
		body, err := json.Marshal(resp)
		if err != nil {
			writeHTTPResult(ctx, nil, err)
			return
		}
		etag := tool.GenerateETag(body)
		ctx.Header("ETag", etag)
		if string(ctx.GetHeader("If-None-Match")) == etag {
			ctx.SetStatusCode(consts.StatusNotModified)
			return
		}
		writeHTTPResult(ctx, resp, nil)
	}
}
