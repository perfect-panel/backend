package storefront

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QuerySubscribeGroupList lists every plan group for the storefront.
func (s *Service) QuerySubscribeGroupList(ctx context.Context) (*dto.QuerySubscribeGroupListResponse, error) {
	total, list, err := s.deps.Plans.QueryGroupList(ctx)
	if err != nil {
		logger.WithContext(ctx).Error("[QuerySubscribeGroupListLogic] get subscribe group list failed: ", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get subscribe group list failed: %v", err.Error())
	}
	groupList := make([]dto.SubscribeGroup, 0)
	if err := mapping.Copy(&groupList, list); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map subscribe groups")
	}
	return &dto.QuerySubscribeGroupListResponse{
		Total: total,
		List:  groupList,
	}, nil
}
