// Package edge is the network module's edge subdomain: the manifest a
// trusted edge-subscribe Worker fetches for a subscription token, listing
// the subscription's state and the proxies of its plan's nodes. Only the
// module facade may reach it.
package edge

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
)

// Snapshot is the per-request view of the runtime-mutable settings the edge
// manifest consumes.
type Snapshot struct {
	Subscribe config.SubscribeConfig
}

// Deps declares the subdomain's dependencies; the module facade reads them
// from the store (DepsFrom).
type Deps struct {
	// Subscriptions resolves the manifest token, Accounts gates on the
	// owner's account, Plans and Nodes select the proxies.
	Subscriptions TokenResolver
	Accounts      AccountStateReader
	Plans         PlanReader
	Nodes         NodeLister
	// Config snapshots the runtime-mutable settings per request.
	Config func() Snapshot
}

// TokenResolver finds the subscription behind a manifest token; an unknown
// token reports gorm.ErrRecordNotFound.
type TokenResolver interface {
	SubscriptionByToken(ctx context.Context, token string) (*usersub.Subscribe, error)
}

// AccountStateReader reads the subscription owner's account state.
type AccountStateReader interface {
	FindAccountState(ctx context.Context, id int64) (*user.AccountState, error)
}

// PlanReader reads the subscription's plan.
type PlanReader interface {
	PlanByID(ctx context.Context, id int64) (*subscribe.Subscribe, error)
}

// SubscriptionReader is the subscription read surface of the manifest (the
// subscription facade): the subscription behind a token and its plan.
type SubscriptionReader interface {
	TokenResolver
	PlanReader
}

// NodeLister lists the nodes a plan's scope selects.
type NodeLister interface {
	ListNodesByScope(ctx context.Context, nodeIDs []int64, tags []string, enabled *bool, preload bool) ([]*node.Node, error)
}

// Service is the edge manifest entry point used by the network facade.
type Service struct {
	deps Deps
}

// NewService returns the edge manifest service over deps.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	Node() repository.NodeRepo
}

// DepsFrom reads the manifest's ports from store, the subscription reads and
// accounts, the identity domain's account gate.
func DepsFrom(store Store, subscriptions SubscriptionReader, accounts AccountStateReader, config func() Snapshot) Deps {
	return Deps{
		Subscriptions: subscriptions,
		Accounts:      accounts,
		Plans:         subscriptions,
		Nodes:         store.Node(),
		Config:        config,
	}
}
