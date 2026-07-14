package ads

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateAdsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Ads
func NewUpdateAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAdsLogic {
	return &UpdateAdsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAdsLogic) UpdateAds(req *dto.UpdateAdsRequest) error {
	data, err := l.svcCtx.Store.Ads().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Errorw("find ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ads error: %v", err.Error())
	}
	tool.DeepCopy(data, req)
	data.StartTime = time.UnixMilli(req.StartTime)
	data.EndTime = time.UnixMilli(req.EndTime)
	if err := l.svcCtx.Store.Ads().Update(l.ctx, data); err != nil {
		l.Errorw("update ads error", logger.Field("error", err.Error()), logger.Field("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ads error: %v", err.Error())
	}
	return nil
}
