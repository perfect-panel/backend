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

// UpdateBindEmail binds a new email address, proven by a code sent to it, to
// the calling account. Replacing the address the account already has also
// needs the current password or, for an account without one, a security
// code sent to the current address; see rebind.
func (s *Service) UpdateBindEmail(ctx context.Context, req *dto.UpdateBindEmailRequest) error {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, identifier.Email); err != nil {
		return err
	}
	domainList, restrict := s.deps.EmailDomains()
	email, err := identifier.ValidateEmail(req.Email, domainList, restrict)
	if err != nil {
		return xerr.Wrapf(err, xerr.InvalidParams, "invalid email")
	}
	req.Email = email
	u, ok := user.FromContext(ctx)
	if !ok {
		return xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	return s.rebind(ctx, u, rebinding{
		authType:    identifier.Email,
		address:     email,
		code:        req.Code,
		codeKey:     verification.EmailCodeKey(auth.Register, email),
		password:    req.Password,
		currentCode: req.CurrentCode,
	})
}
