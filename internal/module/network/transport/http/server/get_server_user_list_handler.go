package server

import (
	"context"

	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/module/network"
	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/transport/http/validation"
	"github.com/perfect-panel/server/pkg/xerr"
)

// ServerUserListReader is the part of the network facade
// GetServerUserListHandler calls.
type ServerUserListReader interface {
	GetServerUserList(ctx context.Context, req *dto.GetServerUserListRequest, meta network.RequestMeta) (*dto.GetServerUserListResponse, network.ResponseMeta, error)
}

var _ ServerUserListReader = network.Service(nil)

// GetServerUserListHandler documents Get user list.
//
// @Summary Get user list
// @Tags node
// @Accept json
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request query dto.GetServerUserListRequest false "Request parameters"
// @Success 200 {object} dto.GetServerUserListResponse
// @Router /v1/server/user [get]
func GetServerUserListHandler(service ServerUserListReader) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		acceptsProtobuf := acceptsProtobuf(ctx)
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req := dto.GetServerUserListRequest{ServerCommon: commonReq}
		if validateErr := validation.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		ifNoneMatch := string(ctx.GetHeader("If-None-Match"))
		resp, respMeta, err := service.GetServerUserList(c, &req, network.RequestMeta{
			IfNoneMatch: ifNoneMatchForRepresentation(ifNoneMatch, acceptsProtobuf),
		})
		writeHeaders(ctx, respMeta.Headers)
		if err != nil {
			if errors.Is(err, network.ErrServerBusy) {
				writeServerReportResult(ctx, err)
				return
			}
			if errors.Is(err, xerr.ErrNotModified) {
				ctx.String(consts.StatusNotModified, "Not Modified")
				return
			}
			writeServerText(ctx, consts.StatusNotFound, "Not Found")
			return
		}
		if acceptsProtobuf {
			if err := writeServerProtobufWithETag(ctx, serverUserListResponseToProtobuf(resp), ifNoneMatch); err != nil {
				writeServerReportResult(ctx, err)
			}
			return
		}
		ctx.JSON(consts.StatusOK, resp)
	}
}
