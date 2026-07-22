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

// QueryServerProtocolConfigHandler documents Get Server Protocol Config.
//
// @Summary Get Server Protocol Config
// @Tags node
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param server_id path int true "Server ID"
// @Param protocols query []string false "Protocols to include" collectionFormat(multi)
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryServerConfigResponse}
// @Router /v2/server/{server_id} [get]
func QueryServerProtocolConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		ctx.Header("Vary", "Accept")
		acceptsProtobuf := acceptsProtobuf(ctx)
		serverID, err := strconv.ParseInt(ctx.Param("server_id"), 10, 64)
		if err != nil {
			logger.WithContext(c).Debugf("[QueryServerProtocolConfigHandler] Parse server_id error: %v, Param: %s", err, ctx.Param("server_id"))
			writeServerText(ctx, consts.StatusBadRequest, "Invalid Params")
			ctx.Abort()
			return
		}
		req := dto.QueryServerConfigRequest{
			ServerID:  serverID,
			SecretKey: ctx.Query("secret_key"),
			Protocols: queryValues(ctx, "protocols", "protocols[]"),
		}
		if svcCtx.Config.Node.NodeSecret != req.SecretKey {
			writeServerText(ctx, consts.StatusUnauthorized, "Unauthorized")
			ctx.Abort()
			return
		}

		l := server.NewQueryServerProtocolConfigLogic(c, svcCtx)
		resp, err := l.QueryServerProtocolConfig(&req)
		if err != nil {
			writeServerReportResult(ctx, err)
			return
		}
		if acceptsProtobuf {
			message, err := queryServerProtocolConfigResponseToProtobuf(resp)
			if err != nil {
				writeServerReportResult(ctx, err)
				return
			}
			if err := writeServerProtobufWithETag(ctx, message, string(ctx.GetHeader("If-None-Match"))); err != nil {
				writeServerReportResult(ctx, err)
			}
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
