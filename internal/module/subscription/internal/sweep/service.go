// Package sweep implements the subscription lifecycle sweep: marking
// traffic-exceeded and expired subscriptions finished, then firing the
// retryable side effects (owner notification, cache invalidation). Only the
// module facade may reach it.
package sweep

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Notifier delivers the lifecycle notices to the subscription owner. The
// composition root adapts it to the notification queue until a notification
// facade owns email delivery.
type Notifier interface {
	NotifySubscriptionExpired(ctx context.Context, email string, expiredAt time.Time)
	NotifyTrafficExceeded(ctx context.Context, email string)
	// NotifySubscriptionExpiring warns the owner before the subscription
	// stops. Renewal amount is in minor units; an empty plan name or a zero
	// amount means the plan could not be read.
	NotifySubscriptionExpiring(ctx context.Context, userID int64, planName string, expireAt time.Time, renewalAmount int64)
}

// OwnerEmailReader is the read-only identity port resolving a user's email
// binding; the composition root backs it with the identity facade.
// Soft-deleted users resolve to no binding.
type OwnerEmailReader interface {
	FindUserAuthMethodsByUserIds(ctx context.Context, method string, userIds []int64) ([]*user.AuthMethods, error)
}

// OwnerStateReader is the read-only identity port reporting whether an owner
// account still exists; the composition root backs it with the identity
// facade.
type OwnerStateReader interface {
	FindAccountState(ctx context.Context, id int64) (*user.AccountState, error)
}

// CacheInvalidator drops cached subscription rows.
type CacheInvalidator interface {
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	UserSubs repository.UserSubscriptionRepo
	Plans    repository.SubscribeRepo
	Cache    CacheInvalidator
	Store    Store
	Emails   OwnerEmailReader
	Owners   OwnerStateReader
	Notify   Notifier
}

// Service is the lifecycle-sweep entry point used by the subscription
// facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// CheckSubscriptions runs both lifecycle sweeps. Each sweep commits its
// status flip in a subscription-domain transaction; notifications and cache
// invalidation are retryable side effects that run after the commit
// (ADR-001 step 2). The sweeps are independent: one failing does not stop the
// other, and the returned error joins both failures so the task records it.
func (s *Service) CheckSubscriptions(ctx context.Context) error {
	logger.WithContext(ctx).Debugf("[CheckSubscription] Start check subscription: %s", timeutil.Now().Format(time.DateTime))
	return errors.Join(
		s.markSubscribes(ctx, usersub.SubscribeStatusFinished, "[Check Subscription Traffic]", s.sendTrafficNotify,
			func(store repository.SubscriptionStore) ([]*usersub.Subscribe, error) {
				return store.UserSubscription().FindTrafficExceededSubscribes(ctx)
			}),
		s.markSubscribes(ctx, usersub.SubscribeStatusExpired, "[Check Subscription Expire]", s.sendExpiredNotify,
			func(store repository.SubscriptionStore) ([]*usersub.Subscribe, error) {
				return store.UserSubscription().FindExpiredSubscribes(ctx, timeutil.Now())
			}),
	)
}

// markSubscribes finishes the subscriptions find selects with status and
// fires the side effects for the ones it finished. A failure is returned,
// named after its sweep, for the task runner's one log line.
func (s *Service) markSubscribes(ctx context.Context, status uint8, tag string, notify func(context.Context, []*usersub.Subscribe), find func(repository.SubscriptionStore) ([]*usersub.Subscribe, error)) error {
	log := logger.WithContext(ctx)
	var selected, finished []*usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		var err error
		selected, finished = nil, nil
		selected, err = find(store)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "query subscriptions to finish")
		}
		if len(selected) == 0 {
			return nil
		}
		ids := subscriptionIDs(selected)
		if err := store.UserSubscription().MarkSubscribesFinished(ctx, ids, status, timeutil.Now()); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "mark subscriptions finished")
		}
		finished, err = finishedSubscriptions(ctx, store, ids, status)
		return err
	})
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "%s sweep", tag)
	}
	if len(selected) == 0 {
		log.Debug(tag + " No subscribe need to update")
		return nil
	}
	if len(finished) == 0 {
		log.Infow(tag+" Every selected subscription was renewed or reset meanwhile", logger.Field("user_subscribe_ids", subscriptionIDs(selected)))
		return nil
	}
	notify(ctx, finished)
	if err := s.deps.Cache.ClearSubscribeCache(ctx, finished...); err != nil {
		log.Errorw(tag+" Clear subscribe cache failed", logger.Field("error", err.Error()))
	}
	s.clearServerCache(ctx, finished...)
	log.Infow(tag+" Update subscribe status",
		logger.Field("user_subscribe_ids", subscriptionIDs(finished)),
		logger.Field("count", int64(len(finished))),
		logger.Field("selected", int64(len(selected))))
	return nil
}

// finishedSubscriptions returns, among the selected ids, the subscriptions
// the finishing statement flipped: the ones now in status, read back in the
// same transaction. The statement checks the expiry or the traffic again, so
// a subscription renewed or reset between the selection and the statement
// stays live and gets no notice of an end it did not reach. The sweep
// selects live rows only and the task lock keeps two sweeps from
// overlapping, so a row in status was finished by this statement.
func finishedSubscriptions(ctx context.Context, store repository.SubscriptionStore, ids []int64, status uint8) ([]*usersub.Subscribe, error) {
	rows, err := store.UserSubscription().FindSubscribesByIds(ctx, ids)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read back the finished subscriptions")
	}
	finished := make([]*usersub.Subscribe, 0, len(rows))
	for _, row := range rows {
		if row != nil && row.Status == status && row.FinishedAt != nil {
			finished = append(finished, row)
		}
	}
	return finished, nil
}

func subscriptionIDs(subs []*usersub.Subscribe) []int64 {
	ids := make([]int64, 0, len(subs))
	for _, sub := range subs {
		if sub != nil {
			ids = append(ids, sub.Id)
		}
	}
	return ids
}

func (s *Service) ownerEmails(ctx context.Context, subs []*usersub.Subscribe) map[int64]string {
	if len(subs) == 0 || s.deps.Emails == nil {
		return nil
	}
	userIDs := make([]int64, 0, len(subs))
	seen := make(map[int64]struct{}, len(subs))
	for _, sub := range subs {
		if sub == nil || sub.UserId <= 0 {
			continue
		}
		if _, ok := seen[sub.UserId]; ok {
			continue
		}
		seen[sub.UserId] = struct{}{}
		userIDs = append(userIDs, sub.UserId)
	}
	methods, err := s.deps.Emails.FindUserAuthMethodsByUserIds(ctx, "email", userIDs)
	if err != nil {
		logger.WithContext(ctx).Errorw("[CheckSubscription] FindUserAuthMethodsByUserIds failed", logger.Field("error", err.Error()), logger.Field("user_count", len(userIDs)))
		return nil
	}
	emails := make(map[int64]string, len(methods))
	for _, method := range methods {
		if method != nil && method.UserId > 0 && method.AuthIdentifier != "" {
			emails[method.UserId] = method.AuthIdentifier
		}
	}
	return emails
}

func (s *Service) sendExpiredNotify(ctx context.Context, subs []*usersub.Subscribe) {
	emails := s.ownerEmails(ctx, subs)
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if email := emails[sub.UserId]; email != "" {
			s.deps.Notify.NotifySubscriptionExpired(ctx, email, sub.ExpireTime)
		}
	}
}

func (s *Service) sendTrafficNotify(ctx context.Context, subs []*usersub.Subscribe) {
	emails := s.ownerEmails(ctx, subs)
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if email := emails[sub.UserId]; email != "" {
			s.deps.Notify.NotifyTrafficExceeded(ctx, email)
		}
	}
}

// clearServerCache drops the node user lists of the finished subscriptions'
// plans, all plans in one call.
func (s *Service) clearServerCache(ctx context.Context, userSubs ...*usersub.Subscribe) {
	planIDs := make([]int64, 0, len(userSubs))
	for _, sub := range userSubs {
		planIDs = append(planIDs, sub.SubscribeId)
	}
	if err := s.deps.Plans.ClearCache(ctx, planIDs...); err != nil {
		logger.WithContext(ctx).Errorw("[CheckSubscription] ClearCache failed", logger.Field("error", err.Error()), logger.Field("subscribe_ids", planIDs))
	}
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.SubscriptionTransactor
	Inbox() repository.InboxRepo
}
