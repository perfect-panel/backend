// Package usersub implements the admin-side user subscription management of
// the subscription module. Only the module facade may reach it.
package usersub

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
)

// The ports onto the identity, network and platform domains the admin views
// use, declared here and provided by the composition root.
type (
	// OwnerReader resolves a subscription's owning account and drops the
	// account's cached projections after its subscriptions changed.
	OwnerReader interface {
		FindOne(ctx context.Context, id int64) (*user.User, error)
		ClearUserCacheOf(ctx context.Context, users ...*user.User) error
	}
	// DeviceReader lists an owner's devices.
	DeviceReader interface {
		QueryDevicePageList(ctx context.Context, userid, subscribeId int64, page, size int) ([]*user.Device, int64, error)
	}
	// TrafficLogReader pages a subscription's traffic records (the network
	// facade).
	TrafficLogReader interface {
		SubscriptionTrafficLogs(ctx context.Context, userID, subscribeID int64, page, size int) ([]*trafficEntity.TrafficLog, int64, error)
	}
	// LogReader filters the audit log.
	LogReader interface {
		FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
	}
	// CacheInvalidator drops cached subscription rows.
	CacheInvalidator interface {
		ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
	}
)

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Users    OwnerReader
	Devices  DeviceReader
	Cache    CacheInvalidator
	Traffic  TrafficLogReader
	Logs     LogReader
	Store    Store
	// SingleModel forbids holding more than one blocking subscription;
	// runtime-mutable, read per request.
	SingleModel func() bool
}

// Service is the user-subscription administration entry point used by the
// subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Store is the persistence capability required by this package: the
// subscription transaction its read-modify-write operations lock the row in.
type Store interface {
	repository.SubscriptionTransactor
}
