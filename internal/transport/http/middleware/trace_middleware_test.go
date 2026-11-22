package middleware

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/pkg/httpx"
)

func TestRequestError_returnsParameterError_whenNativeContextHasRecordedParameterError(t *testing.T) {
	// Given
	ctx := app.NewContext(0)
	httpx.ParamErrorResult(ctx, errors.New("missing token"))

	// When
	got := requestError(ctx)

	// Then
	if got == nil || got.Error() != "missing token" {
		t.Fatalf("expected parameter error %q, got %v", "missing token", got)
	}
}

func TestRequestAttributesExcludeCredentialsAndPersonalMetadata(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Request.SetRequestURI("https://example.test/sub/secret-path?token=query-secret")
	ctx.Request.Header.Set("User-Agent", "private-user-agent")
	ctx.SetFullPath("/sub/:token")

	for _, attr := range requestAttributes(ctx) {
		key := string(attr.Key)
		for _, forbiddenKey := range []string{"url.full", "url.path", "url.query", "user_agent", "client.address", "client.port"} {
			if strings.Contains(key, forbiddenKey) {
				t.Fatalf("sensitive trace attribute %q is present", key)
			}
		}
		value := fmt.Sprint(attr.Value.AsInterface())
		for _, secret := range []string{"secret-path", "query-secret", "private-user-agent"} {
			if strings.Contains(value, secret) {
				t.Fatalf("trace attribute %q contains %q", key, secret)
			}
		}
	}
}
