package kernel

import (
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ModuleConn is the per-connection context handed to a repo builder.
type ModuleConn struct {
	DB    *gorm.DB
	Redis *redis.Client
	// Invalidations batches cache invalidation keys during a transaction;
	// nil outside transactions.
	Invalidations *cache.InvalidationQueue
}

// Conn builds the cached connection every repository implementation wraps.
func (c ModuleConn) Conn() cache.CachedConn {
	if c.Invalidations != nil {
		return cache.NewConn(c.DB, c.Redis, cache.WithInvalidationQueue(c.Invalidations))
	}
	return cache.NewConn(c.DB, c.Redis)
}

// PlatformRepos is the shared-kernel bundle.
type PlatformRepos struct {
	System SystemRepo
	Logs   LogRepo
	Tasks  TaskRepo
	Inbox  InboxRepo
	Outbox OutboxRepo
}

// PlatformBuilder builds the shared-kernel bundle for a connection: the root
// connection or a transaction's.
type PlatformBuilder func(conn ModuleConn) PlatformRepos
