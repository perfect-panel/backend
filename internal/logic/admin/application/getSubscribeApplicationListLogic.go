package application

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetSubscribeApplicationListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetSubscribeApplicationListLogic Get subscribe application list
func NewGetSubscribeApplicationListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeApplicationListLogic {
	return &GetSubscribeApplicationListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeApplicationListLogic) GetSubscribeApplicationList(req *dto.GetSubscribeApplicationListRequest) (resp *dto.GetSubscribeApplicationListResponse, err error) {
	data, err := l.svcCtx.Store.Client().List(l.ctx)
	if err != nil {
		l.Errorf("Failed to get subscribe application list: %v", err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to get subscribe application list")
	}
	var list []dto.SubscribeApplication
	for _, item := range data {
		var temp dto.DownloadLink
		if item.DownloadLink != "" {
			_ = json.Unmarshal([]byte(item.DownloadLink), &temp)
		}
		list = append(list, dto.SubscribeApplication{
			Id:                item.Id,
			Name:              item.Name,
			Description:       item.Description,
			Icon:              item.Icon,
			Scheme:            item.Scheme,
			UserAgent:         item.UserAgent,
			IsDefault:         item.IsDefault,
			SubscribeTemplate: item.SubscribeTemplate,
			OutputFormat:      item.OutputFormat,
			DownloadLink:      temp,
			CreatedAt:         item.CreatedAt.UnixMilli(),
			UpdatedAt:         item.UpdatedAt.UnixMilli(),
		})
	}
	resp = &dto.GetSubscribeApplicationListResponse{
		Total: int64(len(list)),
		List:  list,
	}
	return
}
