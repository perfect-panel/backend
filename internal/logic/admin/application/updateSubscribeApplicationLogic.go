package application

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/client"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateSubscribeApplicationLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateSubscribeApplicationLogic Update subscribe application
func NewUpdateSubscribeApplicationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscribeApplicationLogic {
	return &UpdateSubscribeApplicationLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscribeApplicationLogic) UpdateSubscribeApplication(req *dto.UpdateSubscribeApplicationRequest) (resp *dto.SubscribeApplication, err error) {
	data, err := l.svcCtx.Store.Client().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Errorf("Failed to find subscribe application with ID %d: %v", req.Id, err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to find subscribe application with ID %d", req.Id)
	}
	var link client.DownloadLink
	tool.DeepCopy(&link, req.DownloadLink)
	linkData, err := link.Marshal()
	if err != nil {
		l.Errorf("Failed to marshal download link: %v", err)
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), " Failed to marshal download link")
	}

	data.Name = req.Name
	data.Icon = req.Icon
	data.Description = req.Description
	data.Scheme = req.Scheme
	data.UserAgent = req.UserAgent
	data.IsDefault = req.IsDefault
	data.SubscribeTemplate = req.SubscribeTemplate
	data.OutputFormat = req.OutputFormat
	data.DownloadLink = string(linkData)
	err = l.svcCtx.Store.Client().Update(l.ctx, data)
	if err != nil {
		l.Errorf("Failed to update subscribe application with ID %d: %v", req.Id, err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to update subscribe application with ID %d", req.Id)
	}
	resp = &dto.SubscribeApplication{}
	tool.DeepCopy(&resp, data)
	resp.DownloadLink = req.DownloadLink
	return
}
