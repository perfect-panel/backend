package payment

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get supported payment platform
func GetPaymentPlatformHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := payment.NewGetPaymentPlatformLogic(c, svcCtx)
		resp, err := l.GetPaymentPlatform()
		result.HttpResult(ctx, resp, err)
	}
}
