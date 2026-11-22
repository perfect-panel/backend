package application

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteSubscribeApplication removes a client application.
func (s *Service) DeleteSubscribeApplication(ctx context.Context, req *dto.DeleteSubscribeApplicationRequest) error {
	err := s.deps.Clients.Delete(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorf("Failed to delete subscribe application with ID %d: %v", req.Id, err)
		return xerr.Errorf(xerr.DatabaseDeletedError, "%s", err.Error())
	}
	return nil
}
