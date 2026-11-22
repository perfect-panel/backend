package profile

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateBindMobile binds a new phone number, proven by a code sent to it, to
// the calling account. Replacing the number the account already has also
// needs the current password or, for an account without one, a security
// code sent to the current number; see rebind.
func (s *Service) UpdateBindMobile(ctx context.Context, req *dto.UpdateBindMobileRequest) error {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, identifier.Mobile); err != nil {
		return err
	}
	u, ok := user.FromContext(ctx)
	if !ok {
		return xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	phoneNumber, err := identifier.FormatToE164(req.AreaCode, req.Mobile)
	if err != nil {
		return xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
	}
	return s.rebind(ctx, u, rebinding{
		authType:    identifier.Mobile,
		address:     phoneNumber,
		code:        req.Code,
		codeKey:     verification.MobileCodeKey(auth.Register, phoneNumber),
		password:    req.Password,
		currentCode: req.CurrentCode,
	})
}
