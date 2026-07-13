package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// Create user auth method
func CreateUserAuthMethodHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req types.CreateUserAuthMethodRequest
		_ = httpx.ShouldBind(c, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := user.NewCreateUserAuthMethodLogic(ctx, svcCtx)
		err := l.CreateUserAuthMethod(&req)
		result.HttpResult(c, nil, err)
	}
}
