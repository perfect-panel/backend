package middleware

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const requestActorIDKey = "audit_actor_id"

func LoggerMiddleware(enrichers ...requestmeta.Enricher) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		start := time.Now()
		metadata := requestmeta.New(ctx.ClientIP(), string(ctx.UserAgent()))
		for _, enrich := range enrichers {
			if enrich != nil {
				metadata = enrich(metadata)
			}
		}
		c = requestmeta.With(c, metadata)
		c = logger.ContextWithRequestMetadata(c, metadata)
		ctx.Next(c)

		cost := time.Since(start)
		responseStatus := responseStatus(ctx)
		method := string(ctx.Method())
		route := ctx.FullPath()
		if route == "" {
			// Do not fall back to the raw path: unmatched paths are controlled by
			// the caller and may themselves contain credentials or personal data.
			route = "<unmatched>"
		}

		logs := []logger.LogField{
			logger.Field("status", responseStatus),
			logger.Field("method", method),
			logger.Field("route", route),
			logger.Field("request_bytes", len(ctx.Request.Body())),
			logger.Field("response_bytes", len(ctx.Response.Body())),
			logger.RiskField("client_ip", metadata.ClientIP),
			logger.RiskField("user_agent", metadata.UserAgent),
		}
		if actorID, ok := ctx.Get(requestActorIDKey); ok {
			logs = append(logs, logger.Field("actor_id", actorID))
		}
		if httpx.ParamErrorFromRequestContext(ctx) != nil {
			// Parameter errors can echo rejected input values. Preserve the fact
			// that validation failed without persisting the submitted value.
			logs = append(logs, logger.Field("parameter_error", true))
		}
		logs = append(logs, logger.Field("duration", cost))
		// Handlers answer HTTP 200 with the error code in the body, so the
		// failure is judged by that code: a generic code means the server
		// failed, and its full error chain is logged once, here.
		serverFailed := responseStatus >= 500 && responseStatus <= 599
		if err := httpx.HandlerErrorFromRequestContext(ctx); err != nil {
			code := xerr.CodeOf(err)
			logs = append(logs, logger.Field("error_code", code))
			if xerr.IsGeneric(code) {
				serverFailed = true
				logs = append(logs, logger.Field("error", xerr.Detail(err)))
			}
		}
		if serverFailed {
			logger.WithContext(c).Errorw("HTTP Error", logs...)
		} else {
			logger.WithContext(c).Infow("HTTP Request", logs...)
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
