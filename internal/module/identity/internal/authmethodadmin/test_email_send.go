package authmethodadmin

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/infra/mail"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// TestEmailSend sends a test email through the configured email sender.
func (s *Service) TestEmailSend(ctx context.Context, req *dto.TestEmailSendRequest) error {
	cfg := s.deps.Config()
	client, err := mail.NewSender(cfg.EmailPlatform, cfg.EmailPlatformConfig, cfg.SiteName)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "new email sender")
	}
	err = client.SendContext(ctx, []string{req.Email}, "Test Email Send", "this a test email send by ppanel")
	if err != nil {
		// The administrator is testing the sender, so the failure itself is
		// the answer.
		return fmt.Errorf("send test email: %w", xerr.NewErrCodeMsg(xerr.SenderTestFailed, fmt.Sprintf("send email err: %v", err.Error())))
	}
	return nil
}
