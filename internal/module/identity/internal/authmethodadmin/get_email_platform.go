package authmethodadmin

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mail"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
)

// GetEmailPlatform lists the email providers a sender can be configured
// with.
func (s *Service) GetEmailPlatform(context.Context) (*dto.AuthPlatformResponse, error) {
	return &dto.AuthPlatformResponse{
		List: mail.GetSupportedPlatforms(),
	}, nil
}
