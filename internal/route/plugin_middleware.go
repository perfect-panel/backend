package route

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/perfect-panel/server/internal/middleware"
	usermodel "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func applyPluginRouteMiddleware(ctx context.Context, c *app.RequestContext, svcCtx *svc.ServiceContext, mgr *plugin.Manager, route plugin.RouteRegistration) (context.Context, func(), bool) {
	var after func()
	for _, mwName := range route.Middleware {
		switch mwName {
		case "auth":
			var ok bool
			ctx, ok = applyAuthMiddleware(ctx, c, svcCtx)
			if !ok {
				return ctx, after, false
			}
		case "device":
			var deviceAfter func()
			var ok bool
			ctx, deviceAfter, ok = applyDeviceMiddleware(ctx, c, svcCtx)
			if !ok {
				return ctx, after, false
			}
			if deviceAfter != nil {
				previousAfter := after
				after = func() {
					deviceAfter()
					if previousAfter != nil {
						previousAfter()
					}
				}
			}
		default:
			mw, ok := mgr.FindMiddleware(route.PluginName, mwName)
			if !ok {
				c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": fmt.Sprintf("plugin middleware %q not found", mwName)})
				return ctx, after, false
			}
			if !applyWASMMiddleware(ctx, c, mgr, mw) {
				return ctx, after, false
			}
		}
	}
	return ctx, after, true
}

func applyAuthMiddleware(ctx context.Context, c *app.RequestContext, svcCtx *svc.ServiceContext) (context.Context, bool) {
	requestCtx, err := middleware.AuthenticateRequest(ctx, svcCtx, string(c.GetHeader("Authorization")), string(c.Path()))
	if err != nil {
		result.HttpResult(c, nil, err)
		c.Abort()
		return ctx, false
	}
	return requestCtx, true
}

func applyDeviceMiddleware(ctx context.Context, c *app.RequestContext, svcCtx *svc.ServiceContext) (context.Context, func(), bool) {
	if !svcCtx.Config.Device.Enable {
		return ctx, nil, true
	}
	if ctx.Value(constant.CtxKeyUser) == nil && string(c.GetHeader("Login-Type")) != "" {
		ctx = context.WithValue(ctx, constant.LoginType, string(c.GetHeader("Login-Type")))
	}
	loginType, ok := ctx.Value(constant.LoginType).(string)
	if !ok || loginType != "device" {
		return ctx, nil, true
	}
	if !svcCtx.Config.Device.EnableSecurity {
		return ctx, nil, true
	}
	if svcCtx.Config.Device.SecuritySecret == "" {
		result.HttpResult(c, nil, errors.Wrapf(xerr.NewErrCode(xerr.SecretIsEmpty), "Secret is empty"))
		c.Abort()
		return ctx, nil, false
	}
	if !middleware.DecryptDeviceRequest(c, svcCtx.Config.Device.SecuritySecret) {
		result.HttpResult(c, nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidCiphertext), "Invalid ciphertext"))
		c.Abort()
		return ctx, nil, false
	}
	ctx = context.WithValue(ctx, constant.CtxKeyDeviceSecure, true)
	return ctx, func() {
		middleware.EncryptDeviceResponse(c, svcCtx.Config.Device.SecuritySecret)
		c.Abort()
	}, true
}

func applyWASMMiddleware(ctx context.Context, c *app.RequestContext, mgr *plugin.Manager, mw plugin.MiddlewareRegistration) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, mgr.RequestTimeout())
	defer cancel()
	req := buildPluginHandleRequest(ctx, c)
	resp, err := mgr.CallPluginMiddleware(timeoutCtx, mw.PluginName, mw.Handler, req)
	if err != nil {
		logger.Errorf("plugin %q middleware %q error: %v", mw.PluginName, mw.Handler, err)
		c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		c.Abort()
		return false
	}
	return applyWASMMiddlewareResponse(c, resp)
}

func applyWASMMiddlewareResponse(c *app.RequestContext, resp *pluginv1.MiddlewareResponse) bool {
	if resp == nil {
		return true
	}
	for key, value := range resp.Headers {
		if resp.Action == "modify" {
			c.Request.Header.Set(key, value)
		} else {
			c.Response.Header.Set(key, value)
		}
	}
	if resp.Action != "abort" {
		return true
	}
	c.Response.SetStatusCode(int(resp.Status))
	if len(resp.Body) > 0 {
		c.Response.SetBody(resp.Body)
	}
	c.Abort()
	return false
}

func buildPluginHandleRequest(ctx context.Context, c *app.RequestContext) *pluginv1.HandleRequest {
	body := append([]byte(nil), c.Request.Body()...)
	c.Request.SetBody(body)
	query := make(map[string]*pluginv1.StringList)
	c.QueryArgs().VisitAll(func(key, value []byte) {
		name := string(key)
		values := query[name]
		if values == nil {
			values = &pluginv1.StringList{}
			query[name] = values
		}
		values.Values = append(values.Values, string(value))
	})
	headers := make(map[string]*pluginv1.StringList)
	c.Request.Header.VisitAll(func(key, value []byte) {
		name := string(key)
		values := headers[name]
		if values == nil {
			values = &pluginv1.StringList{}
			headers[name] = values
		}
		values.Values = append(values.Values, string(value))
	})
	reqCtx := &pluginv1.RequestContext{ClientIp: string(c.ClientIP())}
	if userInfo, ok := ctx.Value(constant.CtxKeyUser).(*usermodel.User); ok && userInfo != nil {
		reqCtx.UserId = userInfo.Id
		if userInfo.IsAdmin != nil {
			reqCtx.IsAdmin = *userInfo.IsAdmin
		}
	}
	return &pluginv1.HandleRequest{Method: string(c.Method()), Path: string(c.Path()), Query: query, Headers: headers, Body: body, Context: reqCtx}
}

func writePluginResponse(c *app.RequestContext, resp *pluginv1.HandleResponse) {
	if resp == nil {
		c.Response.SetStatusCode(http.StatusOK)
		return
	}
	status := int(resp.Status)
	if status == 0 {
		status = http.StatusOK
	}
	for key, value := range resp.Headers {
		c.Response.Header.Set(key, value)
	}
	c.Response.SetStatusCode(status)
	if len(resp.Body) > 0 {
		c.Response.SetBody(resp.Body)
	}
}
