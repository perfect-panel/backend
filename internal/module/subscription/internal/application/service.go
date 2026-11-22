// Package application implements the client-application subdomain of the
// subscription module: managing the subscribe clients and previewing their
// delivery templates. Only the module facade may reach it.
package application

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/repository"
)

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Clients repository.ClientRepo
	// Nodes is the network read port the template preview renders.
	Nodes NodeLister
}

// NodeLister is the network read port (the network facade): the enabled
// nodes, with their servers, in sort order.
type NodeLister interface {
	ListEnabledNodes(ctx context.Context, limit int) ([]*node.Node, error)
}

// Service is the client-application entry point used by the subscription
// facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// ClientApplications lists the client applications in their stored order,
// for the other modules' views (the public download page).
func (s *Service) ClientApplications(ctx context.Context) ([]*client.SubscribeApplication, error) {
	return s.deps.Clients.List(ctx)
}
