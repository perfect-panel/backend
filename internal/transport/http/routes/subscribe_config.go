package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	subscriptionHTTP "github.com/perfect-panel/backend/internal/module/subscription/transport/http"
)

func registerSubscribeConfigRoutes(router *server.Hertz, deps Dependencies) {
	subscribePath := deps.runtimeConfig().Subscribe.SubscribePath
	if subscribePath == "" {
		subscribePath = "/v1/subscribe/config"
	}
	subscribeDeps := subscriptionHTTP.SubscribeDeps{Service: deps.Subscription, Config: deps.subscribeConfig}
	router.GET(subscribePath, subscriptionHTTP.SubscribeHandler(subscribeDeps))
	if deps.runtimeConfig().Subscribe.PanDomain {
		router.GET("/", subscriptionHTTP.PanDomainSubscribeHandler(subscribeDeps))
	}
}
