package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
)

// ServerStatusReporter is the part of the network facade
// ServerPushStatusHandler calls.
type ServerStatusReporter interface {
	ServerPushStatus(ctx context.Context, req *dto.ServerPushStatusRequest) error
}

var _ ServerStatusReporter = network.Service(nil)

// ServerPushStatusHandler documents Push server status.
//
// @Summary Push server status
// @Tags node
// @Accept json,application/protobuf
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request body dto.ServerPushStatusRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/server/status [post]
func ServerPushStatusHandler(service ServerStatusReporter) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := dto.ServerPushStatusRequest{}
		if err := bindServerStatusRequest(ctx, &req); err != nil {
			writeParamError(ctx, err)
			return
		}
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req.ServerCommon = commonReq
		req.CertPinSHA256 = string(ctx.GetHeader(certificateSHA256Header))
		if validateErr := validation.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		writeServerReportResult(ctx, service.ServerPushStatus(c, &req))
	}
}
