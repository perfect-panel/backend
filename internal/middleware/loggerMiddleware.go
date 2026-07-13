package middleware

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/result"
)

func LoggerMiddleware(svc *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		start := time.Now()
		ctx.Next(c)

		cost := time.Since(start)
		responseStatus := responseStatus(ctx)
		method := string(ctx.Method())
		path := string(ctx.Path())
		host := string(ctx.Host())

		logs := []logger.LogField{
			{
				Key:   "status",
				Value: responseStatus,
			},
			{
				Key:   "request",
				Value: method + " " + host + string(ctx.URI().RequestURI()),
			},
			{
				Key:   "query",
				Value: string(ctx.URI().QueryString()),
			},
			{
				Key:   "ip",
				Value: ctx.ClientIP(),
			},
			{
				Key:   "user-agent",
				Value: string(ctx.UserAgent()),
			},
		}
		if errMessage := requestErrorMessage(ctx); errMessage != "" {
			logs = append(logs, logger.Field("error", errMessage))
		}
		if shouldLogBody(method, path) {
			logs = append(logs, logger.Field("request_body", string(maskSensitiveFields(ctx.Request.Body(), []string{"password", "old_password", "new_password"}))))
			logs = append(logs, logger.Field("response_body", string(ctx.Response.Body())))
		} else if isBodyMethod(method) && isServerTelemetryPath(path) {
			logs = append(logs, logger.Field("body_omitted", true))
		}
		logs = append(logs, logger.Field("duration", cost))
		if responseStatus >= 500 && responseStatus <= 599 {
			logger.WithContext(c).Errorw("HTTP Error", logs...)
		} else {
			logger.WithContext(c).Infow("HTTP Request", logs...)
		}

		if responseStatus == consts.StatusNotFound {
			logger.WithContext(c).Debugf("404 Not Found: Host:%s Path:%s IsPanDomain:%v", host, path, svc.Config.Subscribe.PanDomain)
		}
	}
}

func responseStatus(ctx *app.RequestContext) int {
	status := ctx.Response.StatusCode()
	if status == 0 {
		return consts.StatusOK
	}
	return status
}

func requestErrorMessage(ctx *app.RequestContext) string {
	if err := result.ParamErrorFromRequestContext(ctx); err != nil {
		return err.Error()
	}
	return ""
}

func shouldLogBody(method, path string) bool {
	return isBodyMethod(method) && !isServerTelemetryPath(path)
}

func isBodyMethod(method string) bool {
	switch method {
	case consts.MethodPost, consts.MethodPut, consts.MethodDelete:
		return true
	default:
		return false
	}
}

func isServerTelemetryPath(path string) bool {
	switch {
	case path == "/v1/server/online":
		return true
	case path == "/v1/server/push":
		return true
	case path == "/v1/server/status":
		return true
	case strings.HasPrefix(path, "/v2/server/"):
		return true
	default:
		return false
	}
}

func maskSensitiveFields(data []byte, fieldsToMask []string) []byte {
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return data
	}

	for _, field := range fieldsToMask {
		if _, exists := jsonData[field]; exists {
			jsonData[field] = "***" // use *** to mask sensitive fields
		}
	}
	maskedData, err := json.Marshal(jsonData)
	if err != nil {
		return data
	}
	return maskedData
}
