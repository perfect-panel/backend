package authmethodadmin

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/infra/sms"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// TestSmsSend sends a test verification code through the configured SMS
// sender.
func (s *Service) TestSmsSend(ctx context.Context, req *dto.TestSmsSendRequest) error {
	cfg := s.deps.Config()
	client, err := sms.NewSender(cfg.MobilePlatform, cfg.MobilePlatformConfig)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "new sms sender")
	}
	err = client.Send(ctx, sms.CodeMessage(req.AreaCode, req.Telephone, "123456"))
	if err != nil {
		// The administrator is testing the sender, so the failure itself is
		// the answer.
		return fmt.Errorf("send test sms: %w", xerr.NewErrCodeMsg(xerr.SenderTestFailed, fmt.Sprintf("send sms err: %v", err.Error())))
	}
	return nil
}
