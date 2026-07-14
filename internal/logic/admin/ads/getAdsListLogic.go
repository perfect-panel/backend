package ads

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetAdsListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Ads List
func NewGetAdsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAdsListLogic {
	return &GetAdsListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAdsListLogic) GetAdsList(req *dto.GetAdsListRequest) (resp *dto.GetAdsListResponse, err error) {
	total, data, err := l.svcCtx.Store.Ads().GetAdsListByPage(l.ctx, req.Page, req.Size, ads.Filter{
		Search: req.Search,
		Status: req.Status,
	})
	if err != nil {
		l.Errorw("get ads list error", logger.Field("error", err.Error()), logger.Field("req", req))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ads list error: %v", err.Error())
	}
	resp = &dto.GetAdsListResponse{
		Total: total,
		List:  make([]dto.Ads, len(data)),
	}
	tool.DeepCopy(&resp.List, data)
	return
}
