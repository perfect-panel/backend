package initialize

import (
	"context"
	"html/template"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server/render"
)

func TestNewConfigServer_rendersInitAndRedirectsUnknownRoutes(t *testing.T) {
	// Given
	engine := newConfigServer()
	templates := template.Must(template.ParseFS(templateFS, "templates/*.html"))

	initRequest := engine.NewContext()
	initRequest.HTMLRender = render.HTMLProduction{Template: templates}
	initRequest.Request.SetRequestURI("/init")
	initRequest.Request.Header.SetMethod(http.MethodGet)

	unknownRequest := engine.NewContext()
	unknownRequest.Request.SetRequestURI("/unknown")
	unknownRequest.Request.Header.SetMethod(http.MethodGet)

	// When
	engine.ServeHTTP(context.Background(), initRequest)
	engine.ServeHTTP(context.Background(), unknownRequest)

	// Then
	if status := initRequest.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("expected init status %d, got %d", http.StatusOK, status)
	}
	if len(initRequest.Response.Body()) == 0 {
		t.Fatal("expected init HTML response body")
	}
	if status := unknownRequest.Response.StatusCode(); status != http.StatusFound {
		t.Fatalf("expected redirect status %d, got %d", http.StatusFound, status)
	}
	if location := string(unknownRequest.Response.Header.Peek("Location")); location != "/init" {
		t.Fatalf("expected redirect location %q, got %q", "/init", location)
	}
}
