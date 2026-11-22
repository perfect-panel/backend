package authn

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// TelephoneResetPassword sets a new password for the account of a phone
// number, proven by a security code sent to it, and signs the account in.
func (s *Service) TelephoneResetPassword(ctx context.Context, req *dto.TelephoneResetPasswordRequest) (*dto.LoginResponse, error) {
	if err := s.policy.VerifyHuman(ctx, registerpolicy.Reset, req.CfToken); err != nil {
		return nil, err
	}
	if err := s.policy.EnsureMethodEnabled(ctx, identifier.Mobile); err != nil {
		return nil, err
	}
	phoneNumber, err := identifier.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
	}
	return s.resetPassword(ctx, passwordReset{
		method:     identifier.Mobile,
		identifier: phoneNumber,
		codeKey:    verification.MobileCodeKey(auth.Security, phoneNumber),
		code:       req.Code,
		password:   req.Password,
		device:     req.Identifier,
	})
}
