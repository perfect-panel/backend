package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// Batch delete subscribe group
func BatchDeleteSubscribeGroupHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.BatchDeleteSubscribeGroupRequest
		_ = httpx.ShouldBind(ctx, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		l := subscribe.NewBatchDeleteSubscribeGroupLogic(c, svcCtx)
		err := l.BatchDeleteSubscribeGroup(&req)
		result.HttpResult(ctx, nil, err)
	}
}
