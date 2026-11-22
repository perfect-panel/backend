// Package nodeaccess serves the subscription side of node access to the
// other modules (ADR-001 rule 4): the subscriptions the network may serve,
// found by token, id or node scope together with their plans, and the cache
// invalidation when an account's access ends. Only the module facade may
// reach it.
package nodeaccess

import (
	"context"
	"strings"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// PlanReader reads the plans: one by id, or those a node scope selects.
type PlanReader interface {
	FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error)
	FindByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*subscribe.Subscribe, error)
}

// SubscriptionStore reads the user subscriptions and drops their cached
// entries.
type SubscriptionStore interface {
	FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error)
	FindOneSubscribeByToken(ctx context.Context, token string) (*usersub.Subscribe, error)
	FindSubscribesByIds(ctx context.Context, ids []int64) ([]*usersub.Subscribe, error)
	FindSubscribeDetailsByIds(ctx context.Context, ids []int64) ([]*usersub.SubscribeDetails, error)
	FindSubscribeDetailsByUserIds(ctx context.Context, userIds []int64) ([]*usersub.SubscribeDetails, error)
	FindUsersSubscribeBySubscribeIds(ctx context.Context, subscribeIds []int64) ([]*usersub.Subscribe, error)
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Plans         PlanReader
	Subscriptions SubscriptionStore
}

// Service is the node-access entry point used by the subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Served is a subscription a node may serve now, with the plan whose speed
// and device limits apply to it.
type Served struct {
	Subscription *usersub.Subscribe
	Plan         *subscribe.Subscribe
}

// SubscriptionByToken returns the subscription holding the token. An unknown
// token reports gorm.ErrRecordNotFound.
func (s *Service) SubscriptionByToken(ctx context.Context, token string) (*usersub.Subscribe, error) {
	return s.deps.Subscriptions.FindOneSubscribeByToken(ctx, token)
}

// SubscriptionByID returns the subscription. An unknown id reports
// gorm.ErrRecordNotFound.
func (s *Service) SubscriptionByID(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	return s.deps.Subscriptions.FindOneSubscribe(ctx, id)
}

// SubscriptionsByIDs returns the subscriptions among the ids that exist.
func (s *Service) SubscriptionsByIDs(ctx context.Context, ids []int64) ([]*usersub.Subscribe, error) {
	return s.deps.Subscriptions.FindSubscribesByIds(ctx, ids)
}

// SubscriptionDetailsByIDs returns the subscriptions among the ids that
// exist, each with its plan.
func (s *Service) SubscriptionDetailsByIDs(ctx context.Context, ids []int64) ([]*usersub.SubscribeDetails, error) {
	return s.deps.Subscriptions.FindSubscribeDetailsByIds(ctx, ids)
}

// PlanByID returns the plan.
func (s *Service) PlanByID(ctx context.Context, id int64) (*subscribe.Subscribe, error) {
	return s.deps.Plans.FindOne(ctx, id)
}

// ServableByNodeScope returns the subscriptions the nodes of a scope may
// serve now: the servable subscriptions (usersub.ServableCondition) of the
// plans selecting any of the node ids or tags, by plan and id, each with its
// plan. It reads and writes nothing else; legacy Pending rows are servable
// as they are.
func (s *Service) ServableByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]Served, error) {
	plans, err := s.deps.Plans.FindByNodeScope(ctx, nodeIDs, tags)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "find the plans of the node scope")
	}
	if len(plans) == 0 {
		return nil, nil
	}
	planIDs := make([]int64, 0, len(plans))
	plansByID := make(map[int64]*subscribe.Subscribe, len(plans))
	for _, plan := range plans {
		planIDs = append(planIDs, plan.Id)
		plansByID[plan.Id] = plan
	}
	subs, err := s.deps.Subscriptions.FindUsersSubscribeBySubscribeIds(ctx, planIDs)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "find the servable subscriptions of plans %v", planIDs)
	}
	served := make([]Served, 0, len(subs))
	for _, sub := range subs {
		if plan := plansByID[sub.SubscribeId]; plan != nil {
			served = append(served, Served{Subscription: sub, Plan: plan})
		}
	}
	return served, nil
}

// ClearUserCaches drops the cached entries (by id, by token and the owners'
// lists) of every subscription the users hold, whatever its status, so the
// tokens of a deleted or disabled account stop resolving from the cache. It
// returns the node scope of the subscriptions' plans: the explicit node ids
// and the node tags, whose node user lists still carry the subscriptions. A
// failed cache deletion is logged; the scope is returned all the same.
func (s *Service) ClearUserCaches(ctx context.Context, userIDs []int64) (nodeIDs []int64, tags []string, err error) {
	userIDs = slicesx.RemoveDuplicateElements(userIDs...)
	if len(userIDs) == 0 {
		return nil, nil, nil
	}
	details, err := s.deps.Subscriptions.FindSubscribeDetailsByUserIds(ctx, userIDs)
	if err != nil {
		return nil, nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find the subscriptions of users %v", userIDs)
	}
	log := logger.WithContext(ctx)
	subs := make([]*usersub.Subscribe, 0, len(details))
	for _, item := range details {
		if item == nil {
			continue
		}
		subs = append(subs, &usersub.Subscribe{Id: item.Id, UserId: item.UserId, Token: item.Token, SubscribeId: item.SubscribeId})
		if item.Subscribe == nil {
			continue
		}
		// The node lists follow the plan's selectors as stored: the explicit
		// ids and the tags split at the commas.
		ids, parseErr := slicesx.ParseInt64CSV(item.Subscribe.Nodes)
		if parseErr != nil {
			log.Errorw("resolve plan nodes while clearing user access caches",
				logger.Field("subscribe_id", item.Subscribe.Id), logger.Field("error", parseErr.Error()))
		}
		nodeIDs = append(nodeIDs, ids...)
		if value := strings.TrimSpace(item.Subscribe.NodeTags); value != "" {
			tags = append(tags, strings.Split(value, ",")...)
		}
	}
	if err := s.deps.Subscriptions.ClearSubscribeCache(ctx, subs...); err != nil {
		log.Errorw("clear the subscription caches of users", logger.Field("user_ids", userIDs), logger.Field("error", err.Error()))
	}
	return slicesx.RemoveDuplicateElements(nodeIDs...), slicesx.RemoveDuplicateElements(tags...), nil
}
