package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
)

// ServerPushStatusHandler documents Push server status.
//
// @Summary Push server status
// @Tags node
// @Accept json,application/protobuf
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request body dto.ServerPushStatusRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/server/status [post]
func ServerPushStatusHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
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
		if validateErr := svcCtx.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		l := server.NewServerPushStatusLogic(c, svcCtx)
		writeServerReportResult(ctx, l.ServerPushStatus(&req))
	}
}
