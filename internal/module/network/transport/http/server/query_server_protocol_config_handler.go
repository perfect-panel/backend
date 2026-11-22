package server

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
)

// ServerProtocolConfigReader is the part of the network facade
// QueryServerProtocolConfigHandler calls.
type ServerProtocolConfigReader interface {
	QueryServerProtocolConfig(ctx context.Context, req *dto.QueryServerConfigRequest) (*dto.QueryServerConfigResponse, error)
}

var _ ServerProtocolConfigReader = network.Service(nil)

// QueryServerProtocolConfigHandler documents Get Server Protocol Config.
//
// @Summary Get Server Protocol Config
// @Tags node
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param server_id path int true "Server ID"
// @Param protocols query []string false "Protocols to include" collectionFormat(multi)
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryServerConfigResponse}
// @Router /v2/server/{server_id} [get]
func QueryServerProtocolConfigHandler(service ServerProtocolConfigReader, nodeSecret func() string) app.HandlerFunc {
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
		if !nodeSecretMatches(nodeSecret(), req.SecretKey) {
			writeServerText(ctx, consts.StatusUnauthorized, "Unauthorized")
			ctx.Abort()
			return
		}

		resp, err := service.QueryServerProtocolConfig(c, &req)
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
		etag := httpx.GenerateETag(body)
		ctx.Header("ETag", etag)
		if string(ctx.GetHeader("If-None-Match")) == etag {
			ctx.SetStatusCode(consts.StatusNotModified)
			return
		}
		writeHTTPResult(ctx, resp, nil)
	}
}
