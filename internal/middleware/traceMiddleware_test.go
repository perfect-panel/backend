package middleware

import (
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/pkg/result"
)

func TestRequestError_returnsParameterError_whenNativeContextHasRecordedParameterError(t *testing.T) {
	// Given
	ctx := app.NewContext(0)
	result.ParamErrorResult(ctx, errors.New("missing token"))

	// When
	got := requestError(ctx)

	// Then
	if got == nil || got.Error() != "missing token" {
		t.Fatalf("expected parameter error %q, got %v", "missing token", got)
	}
}
