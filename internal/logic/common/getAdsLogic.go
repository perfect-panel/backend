package common

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type GetAdsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Ads
func NewGetAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAdsLogic {
	return &GetAdsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAdsLogic) GetAds(req *dto.GetAdsRequest) (resp *dto.GetAdsResponse, err error) {
	// todo: add ads position and device
	status := 1
	_, data, err := l.svcCtx.Store.Ads().GetAdsListByPage(l.ctx, 1, 200, ads.Filter{
		Status: &status,
	})
	if err != nil {
		return nil, err
	}
	resp = &dto.GetAdsResponse{
		List: make([]dto.Ads, len(data)),
	}
	tool.DeepCopy(&resp.List, data)
	return
}
