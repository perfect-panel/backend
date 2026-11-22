package ticket

import (
	"context"
	"strconv"

	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/redis/go-redis/v9"
)

// Every new ticket also opens a forum topic in the Telegram admin group, so
// one user may open only a handful per hour; every reply is mirrored into
// that topic, so replies are capped too — generously, a conversation is
// many replies, but far below what floods the group's bot budget.
const (
	limitWindowSeconds = 3600
	creationQuota      = 5
	followQuota        = 30
	creationKeyPrefix  = "ticket:limit:user:"
	followKeyPrefix    = "ticket:limit:follow:user:"
)

// CreationLimiter caps how many tickets, or replies, one user may write per
// window.
type CreationLimiter interface {
	// Allow takes one permit for the user and reports whether it was
	// granted.
	Allow(ctx context.Context, userID int64) (bool, error)
}

// NewCreationLimiter backs the ticket creation limit with Redis; a nil
// client disables it.
func NewCreationLimiter(rds *redis.Client) CreationLimiter {
	return newPeriodLimiter(rds, creationQuota, creationKeyPrefix)
}

// NewFollowLimiter backs the reply limit with Redis; a nil client disables
// it.
func NewFollowLimiter(rds *redis.Client) CreationLimiter {
	return newPeriodLimiter(rds, followQuota, followKeyPrefix)
}

func newPeriodLimiter(rds *redis.Client, quota int, keyPrefix string) CreationLimiter {
	if rds == nil {
		return nil
	}
	return periodCreationLimiter{limit: ratelimit.NewPeriodLimit(limitWindowSeconds, quota, rds, keyPrefix)}
}

type periodCreationLimiter struct {
	limit *ratelimit.PeriodLimit
}

func (l periodCreationLimiter) Allow(ctx context.Context, userID int64) (bool, error) {
	state, err := l.limit.Take(ctx, strconv.FormatInt(userID, 10))
	if err != nil {
		return false, err
	}
	return l.limit.ParsePermitState(state), nil
}
