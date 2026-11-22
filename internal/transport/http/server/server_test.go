package httpserver

import (
	"context"
	"net/http"
	"testing"

	appconfig "github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/transport/http/routes"
)

func TestServerSecretMiddlewareBlocksMigratedPost(t *testing.T) {
	app := newTestServer("secret")

	status, body := performNativeRequest(app, http.MethodPost, "/v1/server/online?secret_key=wrong")
	if status != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, status)
	}
	if body != "Forbidden" {
		t.Fatalf("expected forbidden body, got %q", body)
	}
}

func TestQueryServerProtocolConfigRejectsInvalidID(t *testing.T) {
	app := newTestServer("secret")

	status, body := performNativeRequest(app, http.MethodGet, "/v2/server/not-a-number?secret_key=secret")
	if status != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, status)
	}
	if body != "Invalid Params" {
		t.Fatalf("expected invalid params body, got %q", body)
	}
}

func TestQueryServerProtocolConfigRejectsInvalidSecret(t *testing.T) {
	app := newTestServer("secret")

	status, body := performNativeRequest(app, http.MethodGet, "/v2/server/1?secret_key=wrong")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
	if body != "Unauthorized" {
		t.Fatalf("expected unauthorized body, got %q", body)
	}
}

// An installation whose node secret has not been provisioned yet must not
// authenticate anyone; a bare `?secret_key=` used to compare equal to it.
func TestServerSecretMiddlewareRejectsUnprovisionedSecret(t *testing.T) {
	app := newTestServer("")

	status, body := performNativeRequest(app, http.MethodPost, "/v1/server/online?secret_key=")
	if status != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, status)
	}
	if body != "Forbidden" {
		t.Fatalf("expected forbidden body, got %q", body)
	}
}

func TestQueryServerProtocolConfigRejectsUnprovisionedSecret(t *testing.T) {
	app := newTestServer("")

	status, body := performNativeRequest(app, http.MethodGet, "/v2/server/1?secret_key=")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
	if body != "Unauthorized" {
		t.Fatalf("expected unauthorized body, got %q", body)
	}
}

func TestCorsPreflightBypassesServerSecretMiddleware(t *testing.T) {
	app := newTestServer("secret")

	ctx := app.Engine().NewContext()
	ctx.Request.SetRequestURI("/v1/server/online")
	ctx.Request.Header.SetMethod(http.MethodOptions)
	ctx.Request.Header.Set("Origin", "https://example.com")
	app.Engine().ServeHTTP(context.Background(), ctx)

	if status := ctx.Response.StatusCode(); status != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, status)
	}
	if origin := string(ctx.Response.Header.Peek("Access-Control-Allow-Origin")); origin != "https://example.com" {
		t.Fatalf("expected CORS origin header, got %q", origin)
	}
}

func newTestServer(secret string) *Server {
	platformService := platform.New(platform.Deps{})
	return New(Dependencies{
		Routes: routes.Dependencies{Config: appconfig.Config{Runtime: appconfig.Runtime{
			Node: appconfig.NodeConfig{
				NodeSecret: secret,
			},
		}}, Platform: platformService},
	}, "127.0.0.1:0", nil)
}

func performNativeRequest(server *Server, method, uri string) (int, string) {
	ctx := server.Engine().NewContext()
	ctx.Request.SetRequestURI(uri)
	ctx.Request.Header.SetMethod(method)
	server.Engine().ServeHTTP(context.Background(), ctx)
	return ctx.Response.StatusCode(), string(ctx.Response.Body())
}
