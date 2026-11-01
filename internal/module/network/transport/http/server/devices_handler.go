package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	serverv1 "github.com/perfect-panel/server/api/server/v1"
	"github.com/perfect-panel/server/internal/module/network"
	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/transport/http/validation"
)

type ServerDeviceService interface {
	GetServerAliveList(context.Context, *dto.GetServerUserListRequest) (*dto.GetServerAliveListResponse, error)
	ClaimServerDevice(context.Context, *dto.ClaimServerDeviceRequest) (*dto.ClaimServerDeviceResponse, error)
}

var _ ServerDeviceService = network.Service(nil)

// GetServerAliveListHandler returns per-subscription global IP counts.
// @Summary Get online device counts
// @Tags node
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request query dto.GetServerUserListRequest false "Server scope"
// @Success 200 {object} dto.GetServerAliveListResponse
// @Router /v1/server/alivelist [get]
func GetServerAliveListHandler(service ServerDeviceService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		common, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req := &dto.GetServerUserListRequest{ServerCommon: common}
		if err := validation.Validate(req); err != nil {
			writeParamError(ctx, err)
			return
		}
		resp, err := service.GetServerAliveList(c, req)
		if err != nil {
			writeServerReportResult(ctx, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		if acceptsProtobuf(ctx) {
			writeServerProtobuf(ctx, consts.StatusOK, &serverv1.GetServerAliveListResponse{Code: 200, Message: "success", Data: &serverv1.ServerAliveListData{Alive: resp.Alive}})
		} else {
			ctx.JSON(consts.StatusOK, resp)
		}
	}
}

// ClaimServerDeviceHandler atomically reserves or renews one source IP.
// @Summary Claim an online device slot
// @Tags node
// @Accept json,application/protobuf
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request body dto.ClaimServerDeviceRequest true "Device claim"
// @Success 200 {object} dto.ClaimServerDeviceResponse
// @Router /v1/server/device [post]
func ClaimServerDeviceHandler(service ServerDeviceService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := &dto.ClaimServerDeviceRequest{}
		if isProtobufRequest(ctx) {
			var message serverv1.ClaimServerDeviceRequest
			if err := ctx.BindProtobuf(&message); err != nil {
				writeParamError(ctx, err)
				return
			}
			req.SID, req.IP = message.UserId, message.Ip
		} else if err := ctx.BindJSON(req); err != nil {
			writeParamError(ctx, err)
			return
		}
		common, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req.ServerCommon = common
		if err := validation.Validate(req); err != nil {
			writeParamError(ctx, err)
			return
		}
		resp, err := service.ClaimServerDevice(c, req)
		if err != nil {
			writeServerReportResult(ctx, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		if acceptsProtobuf(ctx) {
			writeServerProtobuf(ctx, consts.StatusOK, &serverv1.ClaimServerDeviceResponse{Code: 200, Message: "success", Data: &serverv1.ServerDeviceClaimData{Allowed: resp.Allowed, TtlSeconds: resp.TTLSeconds}})
		} else {
			ctx.JSON(consts.StatusOK, resp)
		}
	}
}
