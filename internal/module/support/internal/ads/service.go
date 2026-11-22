// Package ads implements the ads subdomain of the support module. Only the
// module facade (internal/module/support) may reach it.
package ads

import (
	"context"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Service manages the ads for the support facade.
type Service struct {
	repo repository.AdsRepo
}

// NewService builds the ads service on the ads repository.
func NewService(repo repository.AdsRepo) *Service {
	return &Service{repo: repo}
}

// Create stores a new ad; the request carries its schedule in Unix
// milliseconds.
func (s *Service) Create(ctx context.Context, req *dto.CreateAdsRequest) error {
	if err := s.repo.Insert(ctx, &entity.Ads{
		Title:       req.Title,
		Type:        req.Type,
		Content:     req.Content,
		Description: req.Description,
		TargetURL:   req.TargetURL,
		StartTime:   time.UnixMilli(req.StartTime),
		EndTime:     time.UnixMilli(req.EndTime),
		Status:      req.Status,
	}); err != nil {
		logger.WithContext(ctx).Errorw("insert ads error", logger.Field("error", err.Error()), logger.Field("req", req))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "insert ads error: %v", err.Error())
	}
	return nil
}

// Update overwrites an ad with the request; the request carries its schedule
// in Unix milliseconds.
func (s *Service) Update(ctx context.Context, req *dto.UpdateAdsRequest) error {
	data, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("find ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find ads error: %v", err.Error())
	}
	data.Title = req.Title
	data.Type = req.Type
	data.Content = req.Content
	data.Description = req.Description
	data.TargetURL = req.TargetURL
	data.StartTime = time.UnixMilli(req.StartTime)
	data.EndTime = time.UnixMilli(req.EndTime)
	data.Status = req.Status
	if err := s.repo.Update(ctx, data); err != nil {
		logger.WithContext(ctx).Errorw("update ads error", logger.Field("error", err.Error()), logger.Field("req", req))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update ads error: %v", err.Error())
	}
	return nil
}

// Delete removes an ad.
func (s *Service) Delete(ctx context.Context, req *dto.DeleteAdsRequest) error {
	if err := s.repo.Delete(ctx, req.Id); err != nil {
		logger.WithContext(ctx).Errorw("delete ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete ads error: %v", err.Error())
	}
	return nil
}

// GetDetail returns an ad.
func (s *Service) GetDetail(ctx context.Context, req *dto.GetAdsDetailRequest) (*dto.Ads, error) {
	data, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("find ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find ads error: %v", err.Error())
	}
	resp := adsView(data)
	return &resp, nil
}

// List pages the ads for the admin panel.
func (s *Service) List(ctx context.Context, req *dto.GetAdsListRequest) (*dto.GetAdsListResponse, error) {
	total, data, err := s.repo.GetAdsListByPage(ctx, req.Page, req.Size, entity.Filter{
		Search: req.Search,
		Status: req.Status,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("get ads list error", logger.Field("error", err.Error()), logger.Field("req", req))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get ads list error: %v", err.Error())
	}
	return &dto.GetAdsListResponse{
		Total: total,
		List:  adsViews(data),
	}, nil
}

// adsView shows an ad, its times in Unix milliseconds.
func adsView(ad *entity.Ads) dto.Ads {
	return dto.Ads{
		Id:          int(ad.Id),
		Title:       ad.Title,
		Type:        ad.Type,
		Content:     ad.Content,
		Description: ad.Description,
		TargetURL:   ad.TargetURL,
		StartTime:   ad.StartTime.UnixMilli(),
		EndTime:     ad.EndTime.UnixMilli(),
		Status:      ad.Status,
		CreatedAt:   ad.CreatedAt.UnixMilli(),
		UpdatedAt:   ad.UpdatedAt.UnixMilli(),
	}
}

// adsViews shows the ads in their order; no ads is an empty list, not null.
func adsViews(ads []*entity.Ads) []dto.Ads {
	views := make([]dto.Ads, len(ads))
	for i, ad := range ads {
		views[i] = adsView(ad)
	}
	return views
}
