package subscribe

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
)

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	var _ func(*svc.ServiceContext) app.HandlerFunc = BatchDeleteSubscribeGroupHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = BatchDeleteSubscribeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateSubscribeGroupHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateSubscribeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = DeleteSubscribeGroupHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = DeleteSubscribeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetSubscribeDetailsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetSubscribeGroupListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetSubscribeListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ResetAllSubscribeTokenHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = SubscribeSortHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdateSubscribeGroupHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdateSubscribeHandler
}
