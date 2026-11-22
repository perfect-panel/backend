// Package trial grants the registration trial subscription. It consumes the
// identity.user_registered event: the grant runs in a subscription-domain
// transaction, idempotent via the inbox marker, serialized per user by the
// subscription serial lock. Only the module facade may reach it.
package trial

import (
	"context"
	"fmt"
	"uuid"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Consumer is the trial grant's consumer identity: the name it subscribes
// to identity.user_registered with and its inbox markers' consumer. It is
// persisted; renaming it would grant committed trials again.
const Consumer = "subscription.trial_grant"

// Policy is the per-call view of the runtime-mutable trial settings.
type Policy struct {
	Enabled  bool
	PlanID   int64
	Duration int64
	TimeUnit string
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Plans repository.SubscribeRepo
	Cache CacheInvalidator
	Store Store
	// TrialPolicy snapshots the runtime-mutable trial settings per call.
	TrialPolicy func() Policy
}

// CacheInvalidator drops cached subscription rows.
type CacheInvalidator interface {
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
}

// Service is the trial-grant entry point used by the subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// GrantTrial applies the registration trial exactly once for the user. A
// disabled trial still consumes the event (the marker records the decision),
// so a later policy change never retroactively grants trials to old
// registrations — matching the old in-transaction behavior where the policy
// was evaluated at registration time.
func (s *Service) GrantTrial(ctx context.Context, userID int64) error {
	policy := s.deps.TrialPolicy()
	var granted *usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		mark, err := store.Inbox().Find(ctx, Consumer, fmt.Sprintf("%d", userID))
		if err != nil {
			return err
		}
		if mark != nil {
			return nil
		}
		if policy.Enabled {
			// Serialize with fulfillment and other grants for this user.
			if err := store.UserSubscription().LockUserSerial(ctx, userID); err != nil {
				return err
			}
			plan, err := store.Subscribe().FindOne(ctx, policy.PlanID)
			if err != nil {
				return err
			}
			now := timeutil.Now()
			// A misconfigured trial unit fails the grant, and the event is
			// retried once the setting is fixed, instead of granting a
			// trial that ends where it starts.
			unit, err := period.ParseUnit(policy.TimeUnit)
			if err != nil {
				return xerr.Wrapf(err, xerr.ERROR, "trial time unit")
			}
			expireTime, err := period.App().TermEnd(unit, policy.Duration, now)
			if err != nil {
				return xerr.Wrapf(err, xerr.ERROR, "trial term")
			}
			granted = &usersub.Subscribe{
				UserId:      userID,
				OrderId:     0,
				SubscribeId: plan.Id,
				StartTime:   now,
				ExpireTime:  expireTime,
				Traffic:     plan.Traffic,
				Token:       usersub.NewToken(),
				// The node credential is random like every other
				// subscription's: a time-ordered UUID would tell its issue
				// time and leave fewer bits to guess.
				UUID:   uuid.NewV4().String(),
				Status: usersub.SubscribeStatusActive,
			}
			if err := store.UserSubscription().InsertSubscribe(ctx, granted); err != nil {
				return err
			}
		}
		return store.Inbox().Insert(ctx, Consumer, fmt.Sprintf("%d", userID), "")
	})
	if err != nil {
		return err
	}
	if granted != nil {
		if err := s.deps.Cache.ClearSubscribeCache(ctx, granted); err != nil {
			logger.WithContext(ctx).Errorw("[TrialGrant] ClearSubscribeCache failed", logger.Field("error", err.Error()))
		}
		if err := s.deps.Plans.ClearCache(ctx, granted.SubscribeId); err != nil {
			logger.WithContext(ctx).Errorw("[TrialGrant] Clear plan cache failed", logger.Field("error", err.Error()))
		}
	}
	return nil
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.SubscriptionTransactor
}
