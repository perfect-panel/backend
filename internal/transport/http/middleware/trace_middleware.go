package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/trace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// statusByWriter returns a span status code and message for an HTTP status code
// value returned by a server. Status codes in the 400-499 range are not
// returned as errors.
func statusByWriter(code int) (codes.Code, string) {
	if code < 100 || code >= 600 {
		return codes.Error, fmt.Sprintf("Invalid HTTP status code %d", code)
	}
	if code >= 500 {
		return codes.Error, ""
	}
	return codes.Unset, ""
}

func requestAttributes(ctx *app.RequestContext) []attribute.KeyValue {
	protocolName, protocolVersion := protocolParts(ctx.Request.Header.GetProtocol())
	uri := ctx.URI()
	route := ctx.FullPath()
	if route == "" {
		route = "<unmatched>"
	}

	return []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(string(ctx.Method())),
		semconv.HTTPRequestContentLengthKey.Int64(int64(ctx.Request.Header.ContentLength())),
		semconv.URLSchemeKey.String(string(uri.Scheme())),
		semconv.HTTPRouteKey.String(route),

		semconv.NetworkProtocolNameKey.String(protocolName),
		semconv.NetworkProtocolVersionKey.String(protocolVersion),
	}
}

func protocolParts(protocol string) (string, string) {
	if protocol == "" {
		protocol = "HTTP/1.1"
	}
	parts := strings.SplitN(protocol, "/", 2)
	if len(parts) != 2 {
		return strings.ToLower(protocol), ""
	}
	return strings.ToLower(parts[0]), parts[1]
}

func TraceMiddleware() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		tracer := trace.TracerFromContext(c)
		spanName := ctx.FullPath()
		method := string(ctx.Method())

		c, span := tracer.Start(
			c,
			fmt.Sprintf("%s %s", method, spanName),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		)
		defer span.End()

		requestId := trace.TraceIDFromContext(c)

		ctx.Header(trace.RequestIdKey, requestId)

		span.SetAttributes(requestAttributes(ctx)...)
		span.SetAttributes(attribute.String("http.request_id", requestId))

		c = context.WithValue(c, requestctx.CtxKeyRequestHost, string(ctx.Host()))
		ctx.Next(c)

		status := responseStatus(ctx)
		span.SetStatus(statusByWriter(status))
		if status > 0 {
			span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(status))
		}
		if err := requestError(ctx); err != nil {
			// Validation errors may embed rejected values. Preserve the failure
			// classification without exporting the submitted value.
			span.SetStatus(codes.Error, "request parameter validation failed")
			span.SetAttributes(attribute.String("error.type", "validation"))
		}

		span.SetAttributes(semconv.HTTPResponseBodySizeKey.Int(len(ctx.Response.Body())))
	}
}

func requestError(ctx *app.RequestContext) error {
	return httpx.ParamErrorFromRequestContext(ctx)
}
