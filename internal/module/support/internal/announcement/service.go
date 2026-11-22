// Package announcement implements the announcement subdomain of the support
// module. Only the module facade (internal/module/support) may reach it.
package announcement

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Service manages the announcements for the support facade.
type Service struct {
	repo repository.AnnouncementRepo
}

// NewService builds the announcement service on the announcement repository.
func NewService(repo repository.AnnouncementRepo) *Service {
	return &Service{repo: repo}
}

// Create stores a new announcement.
func (s *Service) Create(ctx context.Context, req *dto.CreateAnnouncementRequest) error {
	if err := s.repo.Insert(ctx, &entity.Announcement{
		Title:   req.Title,
		Content: req.Content,
	}); err != nil {
		logger.WithContext(ctx).Errorw("[CreateAnnouncement] Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "create announcement failed: %v", err.Error())
	}
	return nil
}

// Update overwrites an announcement's title and content; each flag changes
// only when the request sets it.
func (s *Service) Update(ctx context.Context, req *dto.UpdateAnnouncementRequest) error {
	info, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateAnnouncement] Query Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "get announcement error: %v", err.Error())
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
	if err := s.repo.Update(ctx, info); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateAnnouncement] Update Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update announcement error: %v", err.Error())
	}
	return nil
}

// Delete removes an announcement.
func (s *Service) Delete(ctx context.Context, req *dto.DeleteAnnouncementRequest) error {
	if err := s.repo.Delete(ctx, req.Id); err != nil {
		logger.WithContext(ctx).Errorw("[DeleteAnnouncement] Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete announcement failed: %v", err.Error())
	}
	return nil
}

// Get returns an announcement.
func (s *Service) Get(ctx context.Context, req *dto.GetAnnouncementRequest) (*dto.Announcement, error) {
	info, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetAnnouncement] Database Error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get announcement error: %v", err.Error())
	}
	resp := announcementView(info)
	return &resp, nil
}

// List pages the announcements for the admin panel.
func (s *Service) List(ctx context.Context, req *dto.GetAnnouncementListRequest) (*dto.GetAnnouncementListResponse, error) {
	total, list, err := s.repo.GetAnnouncementListByPage(ctx, int(req.Page), int(req.Size), entity.Filter{
		Show:   req.Show,
		Pinned: req.Pinned,
		Popup:  req.Popup,
		Search: req.Search,
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetAnnouncementListByPage error: %v", err.Error())
	}
	return &dto.GetAnnouncementListResponse{Total: total, List: announcementViews(list)}, nil
}

// QueryVisible pages the announcements users see: only shown ones,
// whatever the request asks.
func (s *Service) QueryVisible(ctx context.Context, req *dto.QueryAnnouncementRequest) (*dto.QueryAnnouncementResponse, error) {
	enable := true
	total, list, err := s.repo.GetAnnouncementListByPage(ctx, req.Page, req.Size, entity.Filter{
		Show:   &enable,
		Pinned: req.Pinned,
		Popup:  req.Popup,
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetAnnouncementListByPage error: %v", err.Error())
	}
	return &dto.QueryAnnouncementResponse{Total: total, List: announcementViews(list)}, nil
}

// announcementView shows an announcement, its times in Unix milliseconds.
func announcementView(a *entity.Announcement) dto.Announcement {
	return dto.Announcement{
		Id:        a.Id,
		Title:     a.Title,
		Content:   a.Content,
		Show:      a.Show,
		Pinned:    a.Pinned,
		Popup:     a.Popup,
		CreatedAt: a.CreatedAt.UnixMilli(),
		UpdatedAt: a.UpdatedAt.UnixMilli(),
	}
}

// announcementViews shows the announcements in their order; none is an empty
// list, not null.
func announcementViews(list []*entity.Announcement) []dto.Announcement {
	views := make([]dto.Announcement, len(list))
	for i, a := range list {
		views[i] = announcementView(a)
	}
	return views
}
