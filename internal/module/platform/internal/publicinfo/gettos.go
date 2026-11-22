package publicinfo

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetTos returns the terms of service.
func (s *Service) GetTos(ctx context.Context) (*dto.GetTosResponse, error) {
	configs, err := s.deps.Settings.GetTosConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetTos] GetTos error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetTos error: %v", err.Error())
	}
	resp := &dto.GetTosResponse{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
