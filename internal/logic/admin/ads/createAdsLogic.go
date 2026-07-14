package ads

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateAdsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create Ads
func NewCreateAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAdsLogic {
	return &CreateAdsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateAdsLogic) CreateAds(req *dto.CreateAdsRequest) error {
	if err := l.svcCtx.Store.Ads().Insert(l.ctx, &ads.Ads{
		Title:     req.Title,
		Type:      req.Type,
		Content:   req.Content,
		TargetURL: req.TargetURL,
		StartTime: time.UnixMilli(req.StartTime),
		EndTime:   time.UnixMilli(req.EndTime),
		Status:    req.Status,
	}); err != nil {
		l.Errorw("insert ads error: %v", logger.Field("error", err.Error()), logger.Field("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert ads error: %v", err.Error())
	}
	return nil
}
