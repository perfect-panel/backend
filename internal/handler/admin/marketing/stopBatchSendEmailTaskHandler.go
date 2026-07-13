package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/marketing"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// StopBatchSendEmailTaskHandler Stop a batch send email task
func StopBatchSendEmailTaskHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.StopBatchSendEmailTaskRequest
		_ = httpx.ShouldBind(ctx, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		l := marketing.NewStopBatchSendEmailTaskLogic(c, svcCtx)
		err := l.StopBatchSendEmailTask(&req)
		result.HttpResult(ctx, nil, err)
	}
}
