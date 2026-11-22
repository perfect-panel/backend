package marketing

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
)

// handlerFactory compiles only for a factory that builds a native Hertz
// handler from the facade S.
func handlerFactory[S any](func(S) app.HandlerFunc) {}

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	handlerFactory[support.Service](CreateBatchSendEmailTaskHandler)
	handlerFactory[support.Service](CreateQuotaTaskHandler)
	handlerFactory[support.Service](GetBatchSendEmailTaskListHandler)
	handlerFactory[support.Service](GetBatchSendEmailTaskStatusHandler)
	handlerFactory[support.Service](GetPreSendEmailCountHandler)
	handlerFactory[support.Service](QueryQuotaTaskListHandler)
	handlerFactory[support.Service](QueryQuotaTaskPreCountHandler)
	handlerFactory[support.Service](StopBatchSendEmailTaskHandler)
}
