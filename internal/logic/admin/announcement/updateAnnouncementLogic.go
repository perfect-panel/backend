package announcement

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update announcement
func NewUpdateAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAnnouncementLogic {
	return &UpdateAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAnnouncementLogic) UpdateAnnouncement(req *dto.UpdateAnnouncementRequest) error {
	info, err := l.svcCtx.Store.Announcement().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[UpdateAnnouncement] Query Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get announcement error: %v", err.Error())
	}
	info.Title = req.Title
	info.Content = req.Content
	if req.Show != nil {
		info.Show = req.Show
	}
	if req.Pinned != nil {
		info.Pinned = req.Pinned
	}
	if req.Popup != nil {
		info.Popup = req.Popup
	}
	err = l.svcCtx.Store.Announcement().Update(l.ctx, info)
	if err != nil {
		l.Errorw("[UpdateAnnouncement] Update Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update announcement error: %v", err.Error())
	}
	return nil
}
