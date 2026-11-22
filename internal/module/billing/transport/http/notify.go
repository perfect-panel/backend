// Package handler registers the billing module's payment gateway callback
// routes.
package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/billing/transport/http/notify"
	"github.com/perfect-panel/backend/internal/transport/http/middleware"
)

// RegisterNotifyHandlers routes the gateway callbacks. The notify middleware
// resolves the payment method a callback's URL names through the facade
// before the handler settles the callback.
func RegisterNotifyHandlers(router *server.Hertz, service billing.Service) {
	group := router.Group("/v1/notify/")
	group.Use(middleware.NotifyMiddleware(service))
	handler := notify.PaymentNotifyHandler(service)
	group.GET("/:platform/:token", handler)
	group.POST("/:platform/:token", handler)
	group.PUT("/:platform/:token", handler)
	group.DELETE("/:platform/:token", handler)
	group.PATCH("/:platform/:token", handler)
	group.OPTIONS("/:platform/:token", handler)
	group.HEAD("/:platform/:token", handler)
}
