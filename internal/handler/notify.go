package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/server/internal/handler/notify"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func RegisterNotifyHandlers(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/notify/")
	group.Use(middleware.NotifyMiddleware(serverCtx))
	handler := notify.PaymentNotifyHandler(serverCtx)
	group.GET("/:platform/:token", handler)
	group.POST("/:platform/:token", handler)
	group.PUT("/:platform/:token", handler)
	group.DELETE("/:platform/:token", handler)
	group.PATCH("/:platform/:token", handler)
	group.OPTIONS("/:platform/:token", handler)
	group.HEAD("/:platform/:token", handler)
}
