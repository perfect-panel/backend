package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
)

// UserTrafficReporter is the part of the network facade
// ServerPushUserTrafficHandler calls.
type UserTrafficReporter interface {
	ServerPushUserTraffic(ctx context.Context, req *dto.ServerPushUserTrafficRequest) error
}

var _ UserTrafficReporter = network.Service(nil)

// ServerPushUserTrafficHandler documents Push user Traffic.
//
// @Summary Push user Traffic
// @Tags node
// @Accept json,application/protobuf
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request body dto.ServerPushUserTrafficRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/server/push [post]
func ServerPushUserTrafficHandler(service UserTrafficReporter) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := dto.ServerPushUserTrafficRequest{}
		if err := bindUserTrafficRequest(ctx, &req); err != nil {
			writeParamError(ctx, err)
			return
		}
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req.ServerCommon = commonReq
		if validateErr := validation.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		writeServerReportResult(ctx, service.ServerPushUserTraffic(c, &req))
	}
}
