package verifycode

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Every code costs a delivery, and an SMS costs money. The per-address
// quotas alone do not bound a client that rotates addresses, so each client
// address also gets CodesPerIPPerHour codes an hour, whatever the addresses:
// enough for a person retrying a form or binding several addresses, far too
// few for a bombing or pumping script. An unknown address shares one quota.
const (
	CodesPerIPPerHour = 20
	ipQuotaKeyPrefix  = "auth:verify_code_ip:"
)

// takeIPPermit charges one code to the client address's hourly quota.
func (s *Service) takeIPPermit(ctx context.Context) error {
	if s.deps.Redis == nil {
		return nil
	}
	meta, _ := requestmeta.From(ctx)
	ip := meta.ClientIP
	if ip == "" {
		ip = "unknown"
	}
	limiter := ratelimit.NewPeriodLimit(3600, CodesPerIPPerHour, s.deps.Redis, ipQuotaKeyPrefix)
	permit, err := limiter.Take(ctx, ip)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "take the per-address send permit")
	}
	if !limiter.ParsePermitState(permit) {
		return xerr.Errorf(xerr.TooManyRequests, "too many verification codes requested from this address")
	}
	return nil
}

// verifyHuman applies the registration Turnstile challenge to an anonymous
// request for a register code: it starts a registration, and a script must
// not get the deliveries a person gets. A signed-in account binding an
// address, and every security code, prove an account already and skip it.
func (s *Service) verifyHuman(ctx context.Context, verifyType auth.VerifyType, token string) error {
	if verifyType != auth.Register {
		return nil
	}
	if _, signedIn := user.FromContext(ctx); signedIn {
		return nil
	}
	return s.deps.Policy.VerifyHuman(ctx, registerpolicy.Register, token)
}
