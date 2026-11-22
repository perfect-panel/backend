package authn

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The existence checks tell whether an address or number has an account,
// which a sign-up form asks once per address a person types; a script asking
// for many is enumerating accounts. Each client address gets
// ExistenceChecksPerMinute checks per minute.
const (
	ExistenceChecksPerMinute = 20
	existenceCheckKeyPrefix  = "auth:existence_check:"
)

// CheckUser reports whether an account signs in with the email address.
func (s *Service) CheckUser(ctx context.Context, req *dto.CheckUserRequest) (*dto.CheckUserResponse, error) {
	if err := s.takeExistenceCheckPermit(ctx); err != nil {
		return nil, err
	}
	exist, err := s.identityExists(ctx, identifier.Email, req.Email)
	if err != nil {
		return nil, err
	}
	return &dto.CheckUserResponse{Exist: exist}, nil
}

// takeExistenceCheckPermit charges one existence check to the client
// address's minute quota; an unknown address shares one quota.
func (s *Service) takeExistenceCheckPermit(ctx context.Context) error {
	if s.deps.Redis == nil {
		return nil
	}
	meta, _ := requestmeta.From(ctx)
	ip := meta.ClientIP
	if ip == "" {
		ip = "unknown"
	}
	limiter := ratelimit.NewPeriodLimit(60, ExistenceChecksPerMinute, s.deps.Redis, existenceCheckKeyPrefix)
	permit, err := limiter.Take(ctx, ip)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "take the existence check permit")
	}
	if !limiter.ParsePermitState(permit) {
		return xerr.Errorf(xerr.TooManyRequests, "too many account checks from this address")
	}
	return nil
}

func (s *Service) identityExists(ctx context.Context, authType, authIdentifier string) (bool, error) {
	method, err := s.deps.Store.UserAuth().FindUserAuthMethodByOpenID(ctx, authType, authIdentifier)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity", authType)
	}
	return err == nil && method.UserId != 0, nil
}
