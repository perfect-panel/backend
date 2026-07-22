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

// GetServerConfigHandler documents Get server config.
//
// @Summary Get server config
// @Tags node
// @Accept json
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request query dto.GetServerConfigRequest false "Request parameters"
// @Success 200 {object} dto.GetServerConfigResponse
// @Router /v1/server/config [get]
func GetServerConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		acceptsProtobuf := acceptsProtobuf(ctx)
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

		ifNoneMatch := string(ctx.GetHeader("If-None-Match"))
		l := server.NewGetServerConfigLogic(c, svcCtx, server.RequestMeta{
			IfNoneMatch: ifNoneMatchForRepresentation(ifNoneMatch, acceptsProtobuf),
		})
		resp, err := l.GetServerConfig(&req)
		writeHeaders(ctx, l.ResponseMeta().Headers)
		if err != nil {
			if errors.Is(err, xerr.StatusNotModified) {
				ctx.String(consts.StatusNotModified, "Not Modified")
				return
			}
			writeServerText(ctx, consts.StatusNotFound, "Not Found")
			return
		}
		if acceptsProtobuf {
			message, err := serverConfigResponseToProtobuf(resp)
			if err != nil {
				writeServerReportResult(ctx, err)
				return
			}
			if err := writeServerProtobufWithETag(ctx, message, ifNoneMatch); err != nil {
				writeServerReportResult(ctx, err)
			}
			return
		}
		ctx.JSON(consts.StatusOK, resp)
	}
}
