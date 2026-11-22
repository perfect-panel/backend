package app

import (
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/support"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// NewStore assembles the shared store from the module-owned repository
// builders over one connection pool: each module owns its persistence, and a
// module moved to its own database only points its builder at another
// connection (ADR-001).
func NewStore(db *gorm.DB, rds *redis.Client) *repository.GormStore {
	return repository.NewGormStoreWithBuilders(db, rds, repository.Builders{
		Platform:     platform.NewRepoBuilder(),
		Billing:      billing.NewRepoBuilder(),
		Subscription: subscription.NewRepoBuilder(),
		Identity:     identity.NewRepoBuilder(),
		Network:      network.NewRepoBuilder(rds),
		Support:      support.NewRepoBuilder(),
		Notification: notification.NewRepoBuilder(),
	})
}
