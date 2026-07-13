package middleware

import (
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/pkg/result"
)

func TestRequestErrorMessage_returnsParameterError_whenNativeContextHasRecordedParameterError(t *testing.T) {
	// Given
	ctx := app.NewContext(0)
	result.ParamErrorResult(ctx, errors.New("invalid page"))

	// When
	got := requestErrorMessage(ctx)

	// Then
	if got != "invalid page" {
		t.Fatalf("expected parameter error message %q, got %q", "invalid page", got)
	}
}
