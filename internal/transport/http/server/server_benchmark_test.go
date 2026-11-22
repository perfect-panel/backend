package httpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger"
)

func BenchmarkHTTPServerNativeServerForbidden(b *testing.B) {
	benchmarkRequest(b, http.MethodPost, "/v1/server/online?secret_key=wrong", []byte(`{"users":[]}`), http.StatusForbidden)
}

func BenchmarkHTTPServerNativeServerInvalidID(b *testing.B) {
	benchmarkRequest(b, http.MethodGet, "/v2/server/not-a-number?secret_key=secret", nil, http.StatusBadRequest)
}

func BenchmarkHTTPServerNativeCorsPreflight(b *testing.B) {
	benchmarkRequest(b, http.MethodOptions, "/v1/server/online", nil, http.StatusNoContent)
}

func BenchmarkHTTPServerLegacyHeartbeat(b *testing.B) {
	benchmarkRequest(b, http.MethodGet, "/v1/common/heartbeat", nil, http.StatusOK)
}

func benchmarkRequest(b *testing.B, method, uri string, body []byte, expectedStatus int) {
	b.Helper()

	logger.Disable()
	app := newTestServer("secret")
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx := app.Engine().NewContext()
		ctx.Request.SetRequestURI(uri)
		ctx.Request.Header.SetMethod(method)
		if len(body) > 0 {
			ctx.Request.Header.SetContentTypeBytes([]byte("application/json"))
			ctx.Request.SetBody(body)
		}
		app.Engine().ServeHTTP(context.Background(), ctx)
		if status := ctx.Response.StatusCode(); status != expectedStatus {
			b.Fatalf("expected status %d, got %d", expectedStatus, status)
		}
	}
}
