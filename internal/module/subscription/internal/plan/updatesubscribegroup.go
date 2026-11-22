package plan

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateSubscribeGroup renames a plan group and replaces its description.
func (s *Service) UpdateSubscribeGroup(ctx context.Context, req *dto.UpdateSubscribeGroupRequest) error {
	err := s.deps.Plans.UpdateGroup(ctx, &subscribe.Group{
		Id:          req.Id,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		logger.WithContext(ctx).Error("[UpdateSubscribeGroup] update subscribe group failed", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update subscribe group failed: %v", err.Error())
	}
	return nil
}
