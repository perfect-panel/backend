package delivery

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// Subscription fetches are limited per client address. A client polls its
// subscription a few times an hour, so FetchRateQuota fetches per
// FetchRateWindow leave every legitimate client, even one behind a shared
// address, far below the limit, while a token-guessing probe (each guess
// costs a database query and a negative cache entry) is held to one attempt
// per second per address. The address is the one the transport reports: the
// connection's peer, or the client a trusted proxy's forwarding header names.
const (
	FetchRateWindow = time.Minute
	FetchRateQuota  = 60
	// fetchRateKeyPrefix keys the per-address counters in Redis.
	fetchRateKeyPrefix = "subscribe:deliver:ip:"
)

// FetchLimiter grants a subscription fetch per key and reports the permit
// state (ratelimit.Allowed, HitQuota or OverQuota).
type FetchLimiter interface {
	Take(ctx context.Context, key string) (int, error)
}

// NewFetchLimiter returns the per-address fetch limiter over rds.
func NewFetchLimiter(rds *redis.Client) FetchLimiter {
	return ratelimit.NewPeriodLimit(int(FetchRateWindow/time.Second), FetchRateQuota, rds, fetchRateKeyPrefix)
}

// admitFetch takes the client address's fetch permit. Without a limiter, or
// without an address to key it by, every fetch is admitted. A limiter that
// cannot be reached admits the fetch too, with the failure logged: the
// subscriptions must stay deliverable while Redis is down, and the limit only
// slows token guessing down.
func (s *Service) admitFetch(ctx context.Context, clientIP string) error {
	if s.deps.Limiter == nil || clientIP == "" {
		return nil
	}
	state, err := s.deps.Limiter.Take(ctx, clientIP)
	if err != nil {
		logger.WithContext(ctx).Errorw("[SubscribeLogic] Fetch rate limiter unavailable; admitting the fetch", logger.Field("error", err.Error()))
		return nil
	}
	if state == ratelimit.OverQuota {
		logger.WithContext(ctx).Infow("[SubscribeLogic] Fetch rate exceeded", logger.Field("client_ip", clientIP))
		return xerr.Errorf(xerr.TooManyRequests, "too many subscription fetches")
	}
	return nil
}
