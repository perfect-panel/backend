package marketing

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
)

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateBatchSendEmailTaskHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateQuotaTaskHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetBatchSendEmailTaskListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetBatchSendEmailTaskStatusHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetPreSendEmailCountHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = QueryQuotaTaskListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = QueryQuotaTaskPreCountHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = QueryQuotaTaskStatusHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = StopBatchSendEmailTaskHandler
}
