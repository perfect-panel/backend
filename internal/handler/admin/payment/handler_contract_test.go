package payment

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
)

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreatePaymentMethodHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = DeletePaymentMethodHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetPaymentMethodListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetPaymentPlatformHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdatePaymentMethodHandler
}
