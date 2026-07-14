package announcement

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/announcement"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create announcement
func NewCreateAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAnnouncementLogic {
	return &CreateAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateAnnouncementLogic) CreateAnnouncement(req *dto.CreateAnnouncementRequest) error {

	if err := l.svcCtx.Store.Announcement().Insert(l.ctx, &announcement.Announcement{
		Title:   req.Title,
		Content: req.Content,
	}); err != nil {
		l.Errorw("[CreateAnnouncement] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create announcement failed: %v", err.Error())
	}

	return nil
}
