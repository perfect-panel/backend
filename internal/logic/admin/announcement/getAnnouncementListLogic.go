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

type GetAnnouncementListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get announcement list
func NewGetAnnouncementListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAnnouncementListLogic {
	return &GetAnnouncementListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAnnouncementListLogic) GetAnnouncementList(req *dto.GetAnnouncementListRequest) (resp *dto.GetAnnouncementListResponse, err error) {
	total, list, err := l.svcCtx.Store.Announcement().GetAnnouncementListByPage(l.ctx, int(req.Page), int(req.Size), announcement.Filter{
		Show:   req.Show,
		Pinned: req.Pinned,
		Popup:  req.Popup,
		Search: req.Search,
	})
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	resp = &dto.GetAnnouncementListResponse{}
	resp.Total = total
	resp.List = make([]dto.Announcement, 0)
	tool.DeepCopy(&resp.List, list)
	return
}
