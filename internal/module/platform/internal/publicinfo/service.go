// Package publicinfo implements the public-info subdomain of the platform
// module: the unauthenticated site-level reads (global configuration, terms
// of service and privacy policy, aggregate statistics, client downloads,
// liveness). Only the module facade may reach it.
package publicinfo

import (
	"sync"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"golang.org/x/sync/singleflight"
)

// Service is the public-info subdomain entry point used by the platform
// facade.
type Service struct {
	deps Deps
	// statRefresh collapses concurrent refreshes of the site statistics.
	statRefresh singleflight.Group
	// refreshTimeout and resolveTimeout are the budgets of one statistics
	// refresh and of the hostname resolution inside it (statRefreshTimeout
	// and statResolveTimeout); tests shorten them.
	refreshTimeout, resolveTimeout time.Duration
	// statMemo is the process's own copy of the statistics, serving the
	// anonymous callers while Redis is unreachable.
	statMemo statMemo
}

// statMemo remembers the last statistics built or read by this process and
// the last failed refresh, so that a Redis outage does not turn every
// anonymous call into a rebuild (four counts and a resolution of every node
// hostname) and a failing store is asked again only after a pause.
type statMemo struct {
	mu       sync.Mutex
	stat     *dto.GetStatResponse
	at       time.Time
	failedAt time.Time
	err      error
}

// NewService builds the public-info service.
func NewService(deps Deps) *Service {
	return &Service{deps: deps, refreshTimeout: statRefreshTimeout, resolveTimeout: statResolveTimeout}
}
