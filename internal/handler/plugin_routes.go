package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/result"
)

// RegisterPluginHandlers 注册固定插件入口，具体插件路由由 Manager 动态分发。
func RegisterPluginHandlers(router *server.Hertz, svcCtx *svc.ServiceContext, mgr *plugin.Manager) {
	handler := buildPluginDispatcher(svcCtx, mgr)
	registerPluginRoute := func(path string) {
		router.GET(path, handler)
		router.POST(path, handler)
		router.PUT(path, handler)
		router.DELETE(path, handler)
		router.PATCH(path, handler)
		router.OPTIONS(path, handler)
		router.HEAD(path, handler)
	}
	registerPluginRoute("/v1/plugin/:plugin")
	registerPluginRoute("/v1/plugin/:plugin/*path")
	logger.Info("registered plugin dispatcher")
}

func buildPluginDispatcher(svcCtx *svc.ServiceContext, mgr *plugin.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pluginName := c.Param("plugin")
		pluginPath := normalizePluginDispatchPath(c.Param("path"))

		route, ok := mgr.FindRoute(pluginName, string(c.Method()), pluginPath)
		if !ok {
			c.JSON(http.StatusNotFound, map[string]interface{}{
				"error": fmt.Sprintf("plugin route not found: %s %s%s", c.Method(), pluginName, pluginPath),
			})
			return
		}

		requestCtx, after, ok := applyPluginRouteMiddleware(ctx, c, svcCtx, mgr, route)
		if after != nil {
			defer after()
		}
		if !ok {
			return
		}

		timeoutCtx, cancel := context.WithTimeout(requestCtx, mgr.RequestTimeout())
		defer cancel()

		instance := mgr.GetPlugin(pluginName)
		if instance == nil || instance.Pool == nil {
			c.JSON(http.StatusServiceUnavailable, map[string]interface{}{
				"error": fmt.Sprintf("plugin %q is not ready", pluginName),
			})
			return
		}

		req := buildPluginHandleRequest(requestCtx, c)
		resp, err := mgr.CallPlugin(timeoutCtx, pluginName, route.Handler, req)
		if err != nil {
			logger.Errorf("plugin %q handler %q error: %v", pluginName, route.Handler, err)
			result.HttpResult(c, nil, fmt.Errorf("plugin error: %w", err))
			return
		}

		writePluginResponse(c, resp)
	}
}

func normalizePluginDispatchPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "*" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			return "/"
		}
	}
	return path
}
