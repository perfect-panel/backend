package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Query User Affiliate Count
func QueryUserAffiliateHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := user.NewQueryUserAffiliateLogic(c, svcCtx)
		resp, err := l.QueryUserAffiliate()
		result.HttpResult(ctx, resp, err)
	}
}
