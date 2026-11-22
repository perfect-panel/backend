package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
)

// OnlineUsersReporter is the part of the network facade
// PushOnlineUsersHandler calls.
type OnlineUsersReporter interface {
	PushOnlineUsers(ctx context.Context, req *dto.OnlineUsersRequest) error
}

var _ OnlineUsersReporter = network.Service(nil)

// PushOnlineUsersHandler documents Push online users.
//
// @Summary Push online users
// @Tags node
// @Accept json,application/protobuf
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request body dto.OnlineUsersRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/server/online [post]
func PushOnlineUsersHandler(service OnlineUsersReporter) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := dto.OnlineUsersRequest{}
		if err := bindOnlineUsersRequest(ctx, &req); err != nil {
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

		writeServerReportResult(ctx, service.PushOnlineUsers(c, &req))
	}
}
