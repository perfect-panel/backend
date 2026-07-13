package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// Pre Unsubscribe
func PreUnsubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.PreUnsubscribeRequest
		_ = httpx.ShouldBind(ctx, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		l := user.NewPreUnsubscribeLogic(c, svcCtx)
		resp, err := l.PreUnsubscribe(&req)
		result.HttpResult(ctx, resp, err)
	}
}
