package order

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
)

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateOrderHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetOrderListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdateOrderStatusHandler
}
