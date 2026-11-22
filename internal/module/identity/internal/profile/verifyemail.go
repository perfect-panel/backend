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

// VerifyEmail marks the calling account's email binding verified, proven by
// a security code sent to the address.
func (s *Service) VerifyEmail(ctx context.Context, req *dto.VerifyEmailRequest) error {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, identifier.Email); err != nil {
		return err
	}
	domainList, restrict := s.deps.EmailDomains()
	email, err := identifier.ValidateEmail(req.Email, domainList, restrict)
	if err != nil {
		return xerr.Wrapf(err, xerr.InvalidParams, "invalid email")
	}
	cacheKey := verification.EmailCodeKey(auth.Security, email)
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, cacheKey, req.Code, false); err != nil {
		return xerr.Wrapf(err, xerr.VerifyCodeError, "check verification code")
	}

	u, ok := user.FromContext(ctx)
	if !ok {
		return xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	method, err := s.deps.UserAuth.FindUserAuthMethodByOpenID(ctx, identifier.Email, email)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find identity")
	}
	if method.UserId != u.Id {
		return xerr.Errorf(xerr.InvalidAccess, "the email belongs to another account")
	}
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, cacheKey, req.Code, true); err != nil {
		return xerr.Wrapf(err, xerr.VerifyCodeError, "check verification code")
	}
	method.Verified = true
	if err := s.deps.UserAuth.UpdateUserAuthMethods(ctx, method); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update identity")
	}
	return nil
}
