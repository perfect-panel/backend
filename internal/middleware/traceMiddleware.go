package middleware

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/trace"
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
	clientHost, clientPort := remoteAddressParts(ctx.RemoteAddr())
	uri := ctx.URI()

	return []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(string(ctx.Method())),
		semconv.HTTPUserAgentKey.String(string(ctx.UserAgent())),
		semconv.HTTPRequestContentLengthKey.Int64(int64(ctx.Request.Header.ContentLength())),

		semconv.URLFullKey.String(string(uri.FullURI())),
		semconv.URLSchemeKey.String(string(uri.Scheme())),
		semconv.URLFragmentKey.String(string(uri.Hash())),
		semconv.URLPathKey.String(string(uri.Path())),
		semconv.URLQueryKey.String(string(uri.QueryString())),

		semconv.NetworkProtocolNameKey.String(protocolName),
		semconv.NetworkProtocolVersionKey.String(protocolVersion),

		semconv.ClientAddressKey.String(clientHost),
		semconv.ClientPortKey.String(clientPort),
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

func remoteAddressParts(addr net.Addr) (string, string) {
	if addr == nil {
		return "", ""
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String(), ""
	}
	return host, port
}

func TraceMiddleware(_ *svc.ServiceContext) app.HandlerFunc {
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
		span.SetAttributes(
			attribute.String("http.request_id", requestId),
			semconv.HTTPRouteKey.String(ctx.FullPath()),
		)

		c = context.WithValue(c, constant.CtxKeyRequestHost, string(ctx.Host()))
		ctx.Next(c)

		status := responseStatus(ctx)
		span.SetStatus(statusByWriter(status))
		if status > 0 {
			span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(status))
		}
		if err := requestError(ctx); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		span.SetAttributes(semconv.HTTPResponseBodySizeKey.Int(len(ctx.Response.Body())))
	}
}

func requestError(ctx *app.RequestContext) error {
	return result.ParamErrorFromRequestContext(ctx)
}
