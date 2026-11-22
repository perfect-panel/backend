package delivery

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
)

// Config is the runtime configuration snapshot for a delivery request; it is
// re-read per request because the subscribe/site settings are mutable.
type Config struct {
	SiteName string
	// SiteHost is the public site host setting, one host per line; notices
	// show its first host.
	SiteHost              string
	SubscribeDomain       string
	ProfileUpdateInterval int64
	ProfileWebPageURL     string
	UserAgentList         string
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Clients  repository.ClientRepo
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	// Users is the identity read port for the account-enabled gate.
	Users AccountStateReader
	Nodes NodeLister
	Logs  AuditLog
	// ConfigSnapshot reads the current delivery configuration.
	ConfigSnapshot func() Config
	// Limiter bounds the fetches per client address (NewFetchLimiter); nil
	// admits every fetch.
	Limiter FetchLimiter
}

// NodeLister is the network read port (the network facade): the enabled
// nodes, with their servers, a plan's scope selects.
type NodeLister interface {
	ListEnabledNodesByScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error)
}

// AuditLog records the subscription fetches.
type AuditLog interface {
	Insert(ctx context.Context, data *log.SystemLog) error
}

// AccountStateReader reads the subscription owner's account gate from the
// identity domain.
type AccountStateReader interface {
	FindAccountState(ctx context.Context, id int64) (*user.AccountState, error)
}

func (d Deps) config() Config {
	if d.ConfigSnapshot == nil {
		return Config{}
	}
	return d.ConfigSnapshot()
}

// Service is the subscription-delivery entry point used by the
// subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
