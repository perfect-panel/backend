package plan

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateSubscribeGroup stores a new plan group.
func (s *Service) CreateSubscribeGroup(ctx context.Context, req *dto.CreateSubscribeGroupRequest) error {
	err := s.deps.Plans.CreateGroup(ctx, &subscribe.Group{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		logger.WithContext(ctx).Error("[CreateSubscribeGroupLogic] create subscribe group failed: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "create subscribe group failed: %v", err.Error())
	}
	return nil
}
