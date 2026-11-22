package apple

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/app/server/render"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/perfect-panel/backend/pkg/httpx"
)

const (
	TeamID       = "test-team-id"
	ClientID     = "test-client-id"
	KeyID        = "test-key-id"
	ClientSecret = "test-client-secret"
)

func TestAppleRoutes_renderHTMLAndRejectInvalidCallback(t *testing.T) {
	// Given
	router := server.Default()
	templates := template.Must(template.New("apple.html").Parse("{{.title}}: {{.message}}"))
	router.SetHTMLTemplate(templates)
	router.GET("/apple", func(_ context.Context, ctx *app.RequestContext) {
		ctx.HTML(http.StatusOK, "apple.html", utils.H{
			"title":   "Hertz HTML Example",
			"message": "Hello, Hertz!",
		})
	})
	router.POST("/auth/apple/callback", func(_ context.Context, ctx *app.RequestContext) {
		var req CallbackRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			ctx.JSON(http.StatusBadRequest, utils.H{"error": "Invalid request data"})
			return
		}
		handleAppleCallBack(context.Background(), req)
	})
	if err := router.Init(); err != nil {
		t.Fatalf("initialize native Hertz router: %v", err)
	}

	get := router.NewContext()
	get.HTMLRender = render.HTMLProduction{Template: templates}
	get.Request.SetRequestURI("/apple")
	get.Request.Header.SetMethod(http.MethodGet)
	post := router.NewContext()
	post.Request.SetRequestURI("/auth/apple/callback")
	post.Request.Header.SetMethod(http.MethodPost)
	post.Request.Header.Set("Content-Type", "application/json")
	post.Request.SetBodyString("{")

	// When
	router.ServeHTTP(context.Background(), get)
	router.ServeHTTP(context.Background(), post)

	// Then
	if status := get.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("expected HTML status %d, got %d", http.StatusOK, status)
	}
	if status := post.Response.StatusCode(); status != http.StatusBadRequest {
		t.Fatalf("expected invalid callback status %d, got %d", http.StatusBadRequest, status)
	}
	if body := string(post.Response.Body()); body != `{"error":"Invalid request data"}` {
		t.Fatalf("expected invalid callback JSON response, got %q", body)
	}
}

func handleAppleCallBack(ctx context.Context, request CallbackRequest) {
	fmt.Printf("request: %+v\n", request)
	// validate the token
	client, err := New(Config{
		TeamID:       TeamID,
		ClientID:     ClientID,
		KeyID:        KeyID,
		ClientSecret: ClientSecret,
		RedirectURI:  "https://test.ppanel.dev:8443/auth/apple/callback",
	})
	if err != nil {
		fmt.Println("error creating apple client: " + err.Error())
		return
	}
	resp, err := client.VerifyWebToken(ctx, request.Code)
	if err != nil {
		fmt.Println("error verifying token: " + err.Error())
		return
	}
	if resp.Error != "" {
		fmt.Printf("apple returned an error: %s - %s\n", resp.Error, resp.ErrorDescription)
		return
	}

	// Get the unique user ID
	unique, err := GetUniqueID(resp.IDToken)
	if err != nil {
		fmt.Println("error getting unique id: " + err.Error())
		return
	}
	// Get the email
	claim, err := GetClaims(resp.IDToken)
	if err != nil {
		fmt.Println("failed to get claims: " + err.Error())
		return
	}
	email := (*claim)["email"]
	emailVerified := (*claim)["email_verified"]
	isPrivateEmail := (*claim)["is_private_email"]

	// Voila!
	log.Printf("\n unique: %s \n email: %s \n email_verified: %v \n is_private_email: %v", unique, email, emailVerified, isPrivateEmail)
}
