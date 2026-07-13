package document

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/public/document"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get document list
func QueryDocumentListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := document.NewQueryDocumentListLogic(c, svcCtx)
		resp, err := l.QueryDocumentList()
		result.HttpResult(ctx, resp, err)
	}
}
