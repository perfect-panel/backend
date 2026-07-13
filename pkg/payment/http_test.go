package payment

import (
	"context"
	"html/template"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/app/server/render"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

func TestHTTP_rendersStripeHTML_whenRouteIsRequested(t *testing.T) {
	// Given
	router := server.Default()
	templates := template.Must(template.New("stripe.html").Parse("{{.title}}: {{.message}}"))
	router.SetHTMLTemplate(templates)
	router.GET("/stripe", func(_ context.Context, ctx *app.RequestContext) {
		ctx.HTML(http.StatusOK, "stripe.html", utils.H{
			"title":   "Hertz HTML Example",
			"message": "Hello, Hertz!",
		})
	})
	if err := router.Init(); err != nil {
		t.Fatalf("initialize native Hertz router: %v", err)
	}
	request := router.NewContext()
	request.HTMLRender = render.HTMLProduction{Template: templates}
	request.Request.SetRequestURI("/stripe")
	request.Request.Header.SetMethod(http.MethodGet)

	// When
	router.ServeHTTP(context.Background(), request)

	// Then
	if status := request.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("expected HTML status %d, got %d", http.StatusOK, status)
	}
}
