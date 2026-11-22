package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func TestLoggerMiddlewareOmitsRequestAndResponseData(t *testing.T) {
	const (
		pathSecret     = "alice@example.com"
		querySecret    = "query-secret-value"
		requestSecret  = "request-body-secret"
		responseSecret = "response-body-secret"
		parameterValue = "rejected-personal-value"
		clientIP       = "203.0.113.25"
		userAgent      = "RiskClient/1.0 (raw)"
	)

	var output bytes.Buffer
	oldWriter := logger.Reset()
	logger.SetWriter(logger.NewWriter(&output))
	t.Cleanup(func() {
		logger.Reset()
		if oldWriter != nil {
			logger.SetWriter(oldWriter)
		}
	})

	ctx := app.NewContext(0)
	ctx.Request.Header.SetMethod(consts.MethodPost)
	ctx.Request.Header.Set("X-Forwarded-For", clientIP)
	ctx.Request.Header.Set("User-Agent", userAgent)
	ctx.Set(requestActorIDKey, int64(17))
	ctx.Request.SetRequestURI("/unknown/" + pathSecret + "?token=" + querySecret)
	ctx.Request.SetBodyString(`{"password":"` + requestSecret + `"}`)
	ctx.Response.SetStatusCode(consts.StatusBadRequest)
	ctx.Response.SetBodyString(`{"token":"` + responseSecret + `"}`)
	httpx.ParamErrorResult(ctx, &sensitiveValidationError{value: parameterValue})

	LoggerMiddleware(func(metadata requestmeta.Metadata) requestmeta.Metadata {
		metadata.IPCountryCode = "SG"
		metadata.IPCountry = "Singapore"
		metadata.IPCity = "Singapore"
		metadata.IPASN = 64500
		metadata.IPASOrganization = "Example Network"
		return metadata
	})(context.Background(), ctx)

	got := output.String()
	for _, secret := range []string{pathSecret, querySecret, requestSecret, responseSecret, parameterValue} {
		if strings.Contains(got, secret) {
			t.Fatalf("access log contains sensitive value %q: %s", secret, got)
		}
	}
	for _, expected := range []string{`"route":"<unmatched>"`, `"method":"POST"`, `"parameter_error":true`, `"request_bytes":`, `"client_ip":"` + clientIP + `"`, `"user_agent":"` + userAgent + `"`, `"actor_id":17`, `"ip_country_code":"SG"`, `"ip_country":"Singapore"`, `"ip_city":"Singapore"`, `"ip_asn":64500`, `"ip_as_organization":"Example Network"`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("access log is missing safe diagnostic field %q: %s", expected, got)
		}
	}
}

type sensitiveValidationError struct {
	value string
}

func (e *sensitiveValidationError) Error() string {
	return e.value
}

// Handlers answer HTTP 200 and carry the failure in the body code, so the
// access log must judge failures by that code: a server failure is logged at
// error level with its error chain, a client mistake is an ordinary request.
func TestLoggerMiddlewareReportsHandlerErrorsByCode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		wantLevel string
		wantChain bool
	}{
		{"server failure", xerr.Wrapf(errors.New("connection refused"), xerr.DatabaseQueryError, "find order"), `"level":"error"`, true},
		{"client mistake", xerr.NewErrCode(xerr.CouponInsufficientUsage), `"level":"info"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			oldWriter := logger.Reset()
			logger.SetWriter(logger.NewWriter(&output))
			t.Cleanup(func() {
				logger.Reset()
				if oldWriter != nil {
					logger.SetWriter(oldWriter)
				}
			})

			ctx := app.NewContext(0)
			ctx.Request.Header.SetMethod(consts.MethodGet)
			ctx.Request.SetRequestURI("/v1/public/order")
			httpx.HttpResult(ctx, nil, tc.err)
			LoggerMiddleware()(context.Background(), ctx)

			got := output.String()
			if !strings.Contains(got, tc.wantLevel) {
				t.Fatalf("access log level: want %s in %s", tc.wantLevel, got)
			}
			if code := xerr.CodeOf(tc.err); !strings.Contains(got, fmt.Sprintf(`"error_code":%d`, code)) {
				t.Fatalf("access log is missing error_code %d: %s", code, got)
			}
			if gotChain := strings.Contains(got, "connection refused"); gotChain != tc.wantChain {
				t.Fatalf("error chain logged = %v, want %v: %s", gotChain, tc.wantChain, got)
			}
		})
	}
}
