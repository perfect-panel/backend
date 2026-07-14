package announcement

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get announcement
func NewGetAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAnnouncementLogic {
	return &GetAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAnnouncementLogic) GetAnnouncement(req *dto.GetAnnouncementRequest) (resp *dto.Announcement, err error) {
	info, err := l.svcCtx.Store.Announcement().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[GetAnnouncement] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get announcement error: %v", err.Error())
	}
	resp = &dto.Announcement{}
	tool.DeepCopy(resp, info)
	return
}
