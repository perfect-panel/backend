package subscription

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	usersubEntity "github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
)

// Reads is the part of the facade serving the other modules' plan and
// subscription reads that are not about node access (ADR-001 rule 4): the
// checkout's purchase checks, the storefront's plan list, the marketing
// target selection and the bot's views. The reads return the repositories'
// results and errors as they are, so a miss is still gorm.ErrRecordNotFound.
type Reads interface {
	// FilterPlans pages the plans the filter selects.
	FilterPlans(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error)
	// SubscriptionDetailsByID returns the subscription with its plan.
	SubscriptionDetailsByID(ctx context.Context, id int64) (*usersubEntity.SubscribeDetails, error)
	// UserSubscriptions lists the user's subscriptions with their plans;
	// given statuses, only those in one of them.
	UserSubscriptions(ctx context.Context, userID int64, statuses ...int64) ([]*usersubEntity.SubscribeDetails, error)
	// HasBlockingSubscription reports whether the user holds a subscription
	// that forbids buying another while only one may be held.
	HasBlockingSubscription(ctx context.Context, userID int64) (bool, error)
	// CountQuotaConsumingSubscriptions counts the user's subscriptions of the
	// plan that count against its per-user purchase quota.
	CountQuotaConsumingSubscriptions(ctx context.Context, userID, planID int64) (int64, error)
	// SelectSubscriptionIDs returns the ids of the subscriptions the filter
	// selects; CountSelectedSubscriptions counts them.
	SelectSubscriptionIDs(ctx context.Context, filter *usersubEntity.SubscribeFilter) ([]int64, error)
	CountSelectedSubscriptions(ctx context.Context, filter *usersubEntity.SubscribeFilter) (int64, error)
}

// reads serves Reads straight from the module's repositories.
type reads struct {
	plans    repository.SubscribeRepo
	userSubs repository.UserSubscriptionRepo
}

func (r reads) FilterPlans(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error) {
	return r.plans.FilterList(ctx, params)
}

func (r reads) SubscriptionDetailsByID(ctx context.Context, id int64) (*usersubEntity.SubscribeDetails, error) {
	return r.userSubs.FindOneUserSubscribe(ctx, id)
}

func (r reads) UserSubscriptions(ctx context.Context, userID int64, statuses ...int64) ([]*usersubEntity.SubscribeDetails, error) {
	return r.userSubs.QueryUserSubscribe(ctx, userID, statuses...)
}

func (r reads) HasBlockingSubscription(ctx context.Context, userID int64) (bool, error) {
	return r.userSubs.HasBlockingSubscription(ctx, userID)
}

func (r reads) CountQuotaConsumingSubscriptions(ctx context.Context, userID, planID int64) (int64, error) {
	return r.userSubs.CountQuotaConsumingSubscriptions(ctx, userID, planID)
}

func (r reads) SelectSubscriptionIDs(ctx context.Context, filter *usersubEntity.SubscribeFilter) ([]int64, error) {
	return r.userSubs.QuerySubscribeIdsByFilter(ctx, filter)
}

func (r reads) CountSelectedSubscriptions(ctx context.Context, filter *usersubEntity.SubscribeFilter) (int64, error) {
	return r.userSubs.CountSubscribesByFilter(ctx, filter)
}
