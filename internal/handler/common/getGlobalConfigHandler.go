package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// GetGlobalConfigHandler documents Get global config.
//
// @Summary Get global config
// @Tags common
// @Produce json
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetGlobalConfigResponse}
// @Router /v1/common/site/config [get]
func GetGlobalConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		l := common.NewGetGlobalConfigLogic(ctx, common.GetGlobalConfigDependencies{
			Store: svcCtx.Store,
			Config: common.GlobalConfigSnapshot{
				Site:      svcCtx.Config.Site,
				Subscribe: svcCtx.Config.Subscribe,
				Email:     svcCtx.Config.Email,
				Mobile:    svcCtx.Config.Mobile,
				Register:  svcCtx.Config.Register,
				Verify:    svcCtx.Config.Verify,
				Invite:    svcCtx.Config.Invite,
			},
		})
		resp, err := l.GetGlobalConfig()
		result.HttpResult(c, resp, err)
	}
}
