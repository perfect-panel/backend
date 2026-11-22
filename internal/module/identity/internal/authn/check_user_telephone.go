package authn

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CheckUserTelephone reports whether an account signs in with the phone
// number.
func (s *Service) CheckUserTelephone(ctx context.Context, req *dto.TelephoneCheckUserRequest) (*dto.TelephoneCheckUserResponse, error) {
	if err := s.takeExistenceCheckPermit(ctx); err != nil {
		return nil, err
	}
	phoneNumber, err := identifier.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
	}
	exist, err := s.identityExists(ctx, identifier.Mobile, phoneNumber)
	if err != nil {
		return nil, err
	}
	return &dto.TelephoneCheckUserResponse{Exist: exist}, nil
}
