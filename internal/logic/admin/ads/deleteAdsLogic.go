package ads

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteAdsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete Ads
func NewDeleteAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteAdsLogic {
	return &DeleteAdsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteAdsLogic) DeleteAds(req *dto.DeleteAdsRequest) error {
	if err := l.svcCtx.Store.Ads().Delete(l.ctx, req.Id); err != nil {
		l.Errorw("delete ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete ads error: %v", err.Error())
	}
	return nil
}
