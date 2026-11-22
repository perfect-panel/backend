package plan

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// BatchDeleteSubscribeGroup deletes the plan groups.
func (s *Service) BatchDeleteSubscribeGroup(ctx context.Context, req *dto.BatchDeleteSubscribeGroupRequest) error {
	err := s.deps.Plans.BatchDeleteGroup(ctx, req.Ids)
	if err != nil {
		logger.WithContext(ctx).Error("[BatchDeleteSubscribeGroup] Delete Database Error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "batch delete subscribe group failed: %v", err.Error())
	}
	return nil
}
