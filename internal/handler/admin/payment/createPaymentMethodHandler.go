package payment

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// Create Payment Method
func CreatePaymentMethodHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.CreatePaymentMethodRequest
		_ = httpx.ShouldBind(ctx, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		l := payment.NewCreatePaymentMethodLogic(c, svcCtx)
		resp, err := l.CreatePaymentMethod(&req)
		result.HttpResult(ctx, resp, err)
	}
}
