// Package support is the facade of the support module (announcements and,
// as migration proceeds, documents, tickets and ads). Admin and public
// handlers call the same service; access-plane concerns such as auth and
// field trimming stay in the handlers. See docs/adr-001-modular-monolith.md.
package support

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/support/internal/ads"
	"github.com/perfect-panel/server/internal/module/support/internal/announcement"
	"github.com/perfect-panel/server/internal/repository"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	CreateAnnouncement(ctx context.Context, req *dto.CreateAnnouncementRequest) error
	UpdateAnnouncement(ctx context.Context, req *dto.UpdateAnnouncementRequest) error
	DeleteAnnouncement(ctx context.Context, req *dto.DeleteAnnouncementRequest) error
	GetAnnouncement(ctx context.Context, req *dto.GetAnnouncementRequest) (*dto.Announcement, error)
	GetAnnouncementList(ctx context.Context, req *dto.GetAnnouncementListRequest) (*dto.GetAnnouncementListResponse, error)
	// QueryAnnouncement lists announcements visible to end users; Show=true is
	// enforced here, not trusted from the request.
	QueryAnnouncement(ctx context.Context, req *dto.QueryAnnouncementRequest) (*dto.QueryAnnouncementResponse, error)

	CreateAds(ctx context.Context, req *dto.CreateAdsRequest) error
	UpdateAds(ctx context.Context, req *dto.UpdateAdsRequest) error
	DeleteAds(ctx context.Context, req *dto.DeleteAdsRequest) error
	GetAdsDetail(ctx context.Context, req *dto.GetAdsDetailRequest) (*dto.Ads, error)
	GetAdsList(ctx context.Context, req *dto.GetAdsListRequest) (*dto.GetAdsListResponse, error)
}

// Deps declares everything the module needs; the composition root
// (internal/svc) provides them. The module wraps legacy repositories during
// migration and will own its persistence once the domain data moves in
// (ADR-001 step 5).
type Deps struct {
	Announcements repository.AnnouncementRepo
	Ads           repository.AdsRepo
}

func New(deps Deps) Service {
	return &service{
		announcements: announcement.NewService(deps.Announcements),
		ads:           ads.NewService(deps.Ads),
	}
}

type service struct {
	announcements *announcement.Service
	ads           *ads.Service
}

func (s *service) CreateAnnouncement(ctx context.Context, req *dto.CreateAnnouncementRequest) error {
	return s.announcements.Create(ctx, req)
}

func (s *service) UpdateAnnouncement(ctx context.Context, req *dto.UpdateAnnouncementRequest) error {
	return s.announcements.Update(ctx, req)
}

func (s *service) DeleteAnnouncement(ctx context.Context, req *dto.DeleteAnnouncementRequest) error {
	return s.announcements.Delete(ctx, req)
}

func (s *service) GetAnnouncement(ctx context.Context, req *dto.GetAnnouncementRequest) (*dto.Announcement, error) {
	return s.announcements.Get(ctx, req)
}

func (s *service) GetAnnouncementList(ctx context.Context, req *dto.GetAnnouncementListRequest) (*dto.GetAnnouncementListResponse, error) {
	return s.announcements.List(ctx, req)
}

func (s *service) QueryAnnouncement(ctx context.Context, req *dto.QueryAnnouncementRequest) (*dto.QueryAnnouncementResponse, error) {
	return s.announcements.QueryVisible(ctx, req)
}

func (s *service) CreateAds(ctx context.Context, req *dto.CreateAdsRequest) error {
	return s.ads.Create(ctx, req)
}

func (s *service) UpdateAds(ctx context.Context, req *dto.UpdateAdsRequest) error {
	return s.ads.Update(ctx, req)
}

func (s *service) DeleteAds(ctx context.Context, req *dto.DeleteAdsRequest) error {
	return s.ads.Delete(ctx, req)
}

func (s *service) GetAdsDetail(ctx context.Context, req *dto.GetAdsDetailRequest) (*dto.Ads, error) {
	return s.ads.GetDetail(ctx, req)
}

func (s *service) GetAdsList(ctx context.Context, req *dto.GetAdsListRequest) (*dto.GetAdsListResponse, error) {
	return s.ads.List(ctx, req)
}
