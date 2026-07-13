package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
)

func TestTelegramHandler_abortsAndWritesSuccessEnvelope_whenSecretIsInvalid(t *testing.T) {
	// Given
	engine := server.Default()
	engine.POST("/v1/telegram/webhook", TelegramHandler(&svc.ServiceContext{
		Config: appconfig.Config{Telegram: appconfig.Telegram{BotToken: "bot-token"}},
	}))
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/v1/telegram/webhook?secret=invalid")
	ctx.Request.Header.SetMethod(http.MethodPost)

	// When
	engine.ServeHTTP(context.Background(), ctx)

	// Then
	if got := ctx.Response.StatusCode(); got != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, got)
	}
	var response struct {
		Code uint32 `json:"code"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Code != 200 {
		t.Fatalf("expected success envelope code 200, got %d", response.Code)
	}
}
