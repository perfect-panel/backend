// Package ads implements the ads subdomain of the support module. Only the
// module facade (internal/module/support) may reach it.
package ads

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	entity "github.com/perfect-panel/server/internal/model/entity/ads"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type Service struct {
	repo repository.AdsRepo
}

func NewService(repo repository.AdsRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req *dto.CreateAdsRequest) error {
	if err := s.repo.Insert(ctx, &entity.Ads{
		Title:     req.Title,
		Type:      req.Type,
		Content:   req.Content,
		TargetURL: req.TargetURL,
		StartTime: time.UnixMilli(req.StartTime),
		EndTime:   time.UnixMilli(req.EndTime),
		Status:    req.Status,
	}); err != nil {
		logger.WithContext(ctx).Errorw("insert ads error: %v", logger.Field("error", err.Error()), logger.Field("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert ads error: %v", err.Error())
	}
	return nil
}

func (s *Service) Update(ctx context.Context, req *dto.UpdateAdsRequest) error {
	data, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("find ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ads error: %v", err.Error())
	}
	tool.DeepCopy(data, req)
	data.StartTime = time.UnixMilli(req.StartTime)
	data.EndTime = time.UnixMilli(req.EndTime)
	if err := s.repo.Update(ctx, data); err != nil {
		logger.WithContext(ctx).Errorw("update ads error", logger.Field("error", err.Error()), logger.Field("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ads error: %v", err.Error())
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, req *dto.DeleteAdsRequest) error {
	if err := s.repo.Delete(ctx, req.Id); err != nil {
		logger.WithContext(ctx).Errorw("delete ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete ads error: %v", err.Error())
	}
	return nil
}

func (s *Service) GetDetail(ctx context.Context, req *dto.GetAdsDetailRequest) (*dto.Ads, error) {
	data, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("find ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ads error: %v", err.Error())
	}
	resp := new(dto.Ads)
	tool.DeepCopy(resp, data)
	return resp, nil
}

func (s *Service) List(ctx context.Context, req *dto.GetAdsListRequest) (*dto.GetAdsListResponse, error) {
	total, data, err := s.repo.GetAdsListByPage(ctx, req.Page, req.Size, entity.Filter{
		Search: req.Search,
		Status: req.Status,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("get ads list error", logger.Field("error", err.Error()), logger.Field("req", req))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ads list error: %v", err.Error())
	}
	resp := &dto.GetAdsListResponse{
		Total: total,
		List:  make([]dto.Ads, len(data)),
	}
	tool.DeepCopy(&resp.List, data)
	return resp, nil
}
