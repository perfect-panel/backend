// Package adminserver is the network module's admin-side management of the
// servers and nodes: their creation, updates and removal, their sort order,
// the protocols a server offers and its node configuration overrides, and the
// node-facing caches these changes invalidate. Only the module facade may
// reach it.
package adminserver

import (
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/repository"
)

// Snapshot is the per-request view of the runtime-mutable node settings.
type Snapshot struct {
	Node config.NodeConfig
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Store Store
	// Subscriptions is the subscription read port (the subscription
	// facade): the subscriptions behind the online users.
	Subscriptions OnlineSubscriptionReader
	// Config snapshots the runtime-mutable node settings per request.
	Config func() Snapshot
}

// Service is the admin server-management entry point used by the network
// facade.
type Service struct {
	deps Deps
}

// NewService returns the subdomain's service over deps.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.NetworkTransactor
	Node() repository.NodeRepo
}
