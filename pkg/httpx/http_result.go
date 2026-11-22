// Package httpx holds the HTTP conventions the handlers share: binding a
// request's query, form and JSON values (ShouldBind), the response envelope
// and ETags. Every response is HTTP 200 with the outcome in the body, where
// the code is 200 on success and otherwise that of the first xerr.CodeError
// in the error chain (ERROR when there is none); so that the access log
// still sees a failure, the error is also recorded on the request context.
package httpx

import (
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/perfect-panel/backend/pkg/xerr"
)

// HTTPResult is a response before it is written: its status and envelope.
type HTTPResult struct {
	StatusCode int
	Body       any
}

const (
	paramErrorContextKey   = "__ppanel_param_error"
	handlerErrorContextKey = "__ppanel_handler_error"
)

type paramErrorState struct {
	err error
}

type handlerErrorState struct {
	err error
}

// BuildHTTPResult builds the response for a handler's result: resp in the
// success envelope, or the code and message of the first xerr.CodeError in
// err's chain. Any other error is reported as ERROR with a generic message,
// so its text never reaches the client.
func BuildHTTPResult(resp any, err error) HTTPResult {
	if err == nil {
		return HTTPResult{
			StatusCode: http.StatusOK,
			Body:       Success(resp),
		}
	}

	code := xerr.ERROR
	msg := "Internal Server Error"

	// The first code in the chain is the one to report: xerr.Wrapf only adds
	// an outer code when it is more specific than the one it wraps.
	var e *xerr.CodeError
	if errors.As(err, &e) {
		code = e.GetErrCode()
		msg = e.GetErrMsg()
	}

	return HTTPResult{
		StatusCode: http.StatusOK,
		Body:       Error(code, msg),
	}
}

// BuildParamErrorResult builds the response for a request that failed
// binding or validation: InvalidParams with err's message, which tells the
// client what to fix.
func BuildParamErrorResult(err error) HTTPResult {
	return HTTPResult{
		StatusCode: http.StatusOK,
		Body:       Error(xerr.InvalidParams, err.Error()),
	}
}

// HttpResult writes the response for a handler's result (see
// BuildHTTPResult) and records a failure for the access log.
func HttpResult(ctx *app.RequestContext, resp any, err error) {
	if err != nil {
		// Every response is HTTP 200 with the code in the body, so the access
		// log learns about the failure from here.
		ctx.Set(handlerErrorContextKey, handlerErrorState{err: err})
	}
	result := BuildHTTPResult(resp, err)
	ctx.JSON(result.StatusCode, result.Body)
}

// HandlerErrorFromRequestContext returns the error HttpResult reported for
// ctx, if any.
func HandlerErrorFromRequestContext(ctx *app.RequestContext) error {
	value, ok := ctx.Get(handlerErrorContextKey)
	if !ok {
		return nil
	}
	state, ok := value.(handlerErrorState)
	if !ok {
		return nil
	}
	return state.err
}

// ParamErrorResult writes the response for a request that failed binding
// or validation (see BuildParamErrorResult) and records the error for the
// access log.
func ParamErrorResult(ctx *app.RequestContext, err error) {
	recordedErr := errors.New(err.Error())
	recordParamError(ctx, recordedErr)
	result := BuildParamErrorResult(err)
	ctx.JSON(result.StatusCode, result.Body)
}

// ParamErrorFromRequestContext returns the parameter error recorded for ctx.
func ParamErrorFromRequestContext(ctx *app.RequestContext) error {
	value, ok := ctx.Get(paramErrorContextKey)
	if !ok {
		return nil
	}
	state, ok := value.(paramErrorState)
	if !ok {
		return nil
	}
	return state.err
}

func recordParamError(ctx *app.RequestContext, err error) {
	ctx.Set(paramErrorContextKey, paramErrorState{err: err})
}
