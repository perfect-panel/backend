package result

import (
	stderrors "errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func TestBuildHTTPResultSuccess(t *testing.T) {
	data := map[string]string{"status": "ok"}

	result := BuildHTTPResult(data, nil)

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseSuccessBean)
	if !ok {
		t.Fatalf("expected success body, got %T", result.Body)
	}
	if body.Code != http.StatusOK {
		t.Fatalf("expected code %d, got %d", http.StatusOK, body.Code)
	}
	if body.Msg != "success" {
		t.Fatalf("expected success message, got %q", body.Msg)
	}
	if !reflect.DeepEqual(body.Data, data) {
		t.Fatalf("expected data to be preserved")
	}
}

func TestBuildHTTPResultCodeError(t *testing.T) {
	err := errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "wrapped")

	result := BuildHTTPResult(nil, err)

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseErrorBean)
	if !ok {
		t.Fatalf("expected error body, got %T", result.Body)
	}
	if body.Code != xerr.InvalidParams {
		t.Fatalf("expected code %d, got %d", xerr.InvalidParams, body.Code)
	}
	if body.Msg != xerr.MapErrMsg(xerr.InvalidParams) {
		t.Fatalf("expected mapped message %q, got %q", xerr.MapErrMsg(xerr.InvalidParams), body.Msg)
	}
}

func TestBuildHTTPResultGenericError(t *testing.T) {
	result := BuildHTTPResult(nil, stderrors.New("boom"))

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseErrorBean)
	if !ok {
		t.Fatalf("expected error body, got %T", result.Body)
	}
	if body.Code != xerr.ERROR {
		t.Fatalf("expected code %d, got %d", xerr.ERROR, body.Code)
	}
	if body.Msg != "Internal Server Error" {
		t.Fatalf("expected internal server error, got %q", body.Msg)
	}
}

func TestBuildParamErrorResult(t *testing.T) {
	result := BuildParamErrorResult(stderrors.New("bad param"))

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseErrorBean)
	if !ok {
		t.Fatalf("expected error body, got %T", result.Body)
	}
	if body.Code != xerr.InvalidParams {
		t.Fatalf("expected code %d, got %d", xerr.InvalidParams, body.Code)
	}
	if body.Msg != "bad param" {
		t.Fatalf("expected param message, got %q", body.Msg)
	}
}

func TestHttpResult_writesSuccessEnvelope_whenGivenNativeRequestContext(t *testing.T) {
	// Given
	ctx := app.NewContext(0)

	// When
	HttpResult(ctx, map[string]string{"status": "ok"}, nil)

	// Then
	if ctx.Response.StatusCode() != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, ctx.Response.StatusCode())
	}
	if got, want := string(ctx.Response.Body()), `{"code":200,"msg":"success","data":{"status":"ok"}}`; got != want {
		t.Fatalf("expected response %q, got %q", want, got)
	}
}

func TestParamErrorResult_recordsErrorAndWritesEnvelope_whenGivenNativeRequestContext(t *testing.T) {
	// Given
	ctx := app.NewContext(0)
	err := stderrors.New("bad param")

	// When
	ParamErrorResult(ctx, err)

	// Then
	if ctx.Response.StatusCode() != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, ctx.Response.StatusCode())
	}
	if got, want := string(ctx.Response.Body()), `{"code":400,"msg":"bad param"}`; got != want {
		t.Fatalf("expected response %q, got %q", want, got)
	}
	if got := ParamErrorFromRequestContext(ctx); got == nil || got.Error() != err.Error() {
		t.Fatalf("expected recorded parameter error %q, got %v", err, got)
	}
}
