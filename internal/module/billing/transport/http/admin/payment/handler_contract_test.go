package payment

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
)

// handlerFactory compiles only for a factory that builds a native Hertz
// handler from S.
func handlerFactory[S any](func(S) app.HandlerFunc) {}

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	handlerFactory[billing.Service](CreatePaymentMethodHandler)
	handlerFactory[billing.Service](DeletePaymentMethodHandler)
	handlerFactory[billing.Service](GetPaymentMethodListHandler)
	handlerFactory[billing.Service](GetPaymentPlatformHandler)
	handlerFactory[billing.Service](UpdatePaymentMethodHandler)
}
