package plan

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteSubscribeGroup deletes a plan group.
func (s *Service) DeleteSubscribeGroup(ctx context.Context, req *dto.DeleteSubscribeGroupRequest) error {
	err := s.deps.Plans.DeleteGroup(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Error("[DeleteSubscribeGroupLogic] delete subscribe group failed: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete subscribe group failed: %v", err.Error())
	}
	return nil
}
