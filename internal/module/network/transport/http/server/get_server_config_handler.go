package server

import (
	"context"

	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ServerConfigReader is the part of the network facade
// GetServerConfigHandler calls.
type ServerConfigReader interface {
	GetServerConfig(ctx context.Context, req *dto.GetServerConfigRequest, meta network.RequestMeta) (*dto.GetServerConfigResponse, network.ResponseMeta, error)
}

var _ ServerConfigReader = network.Service(nil)

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
func GetServerConfigHandler(service ServerConfigReader) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		acceptsProtobuf := acceptsProtobuf(ctx)
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req := dto.GetServerConfigRequest{ServerCommon: commonReq}
		if validateErr := validation.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		ifNoneMatch := string(ctx.GetHeader("If-None-Match"))
		resp, respMeta, err := service.GetServerConfig(c, &req, network.RequestMeta{
			IfNoneMatch: ifNoneMatchForRepresentation(ifNoneMatch, acceptsProtobuf),
		})
		writeHeaders(ctx, respMeta.Headers)
		if err != nil {
			if errors.Is(err, xerr.ErrNotModified) {
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
