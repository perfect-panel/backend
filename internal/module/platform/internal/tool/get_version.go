package tool

import (
	"context"

	"github.com/perfect-panel/backend/internal/app/buildinfo"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

// GetVersion reports the version the server was built as.
func (s *Service) GetVersion(_ context.Context) (*dto.VersionResponse, error) {
	return &dto.VersionResponse{Version: buildinfo.Display()}, nil
}
