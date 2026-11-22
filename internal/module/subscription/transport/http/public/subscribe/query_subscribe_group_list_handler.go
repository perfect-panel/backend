// Package subscribe holds the public HTTP handlers for the plan storefront
// and the owner's subscription nodes.
package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// Get subscribe group list
func QuerySubscribeGroupListHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QuerySubscribeGroupList(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
