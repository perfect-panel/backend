package announcement

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/announcement"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query announcement
func NewQueryAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryAnnouncementLogic {
	return &QueryAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryAnnouncementLogic) QueryAnnouncement(req *dto.QueryAnnouncementRequest) (resp *dto.QueryAnnouncementResponse, err error) {
	enable := true
	total, list, err := l.svcCtx.Store.Announcement().GetAnnouncementListByPage(l.ctx, req.Page, req.Size, announcement.Filter{
		Show:   &enable,
		Pinned: req.Pinned,
		Popup:  req.Popup,
	})
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	resp = &dto.QueryAnnouncementResponse{}
	resp.Total = total
	resp.List = make([]dto.Announcement, 0)
	tool.DeepCopy(&resp.List, list)
	return
}
