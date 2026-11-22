package authmethodadmin

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/sms"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
)

// GetSmsPlatform lists the SMS providers a sender can be configured with.
func (s *Service) GetSmsPlatform(context.Context) (*dto.AuthPlatformResponse, error) {
	return &dto.AuthPlatformResponse{
		List: sms.GetSupportedPlatforms(),
	}, nil
}
