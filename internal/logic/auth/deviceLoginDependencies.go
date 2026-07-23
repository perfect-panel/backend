package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// DeviceRegistrationPolicy contains the registration checks needed when a
// previously unseen device creates an account.
type DeviceRegistrationPolicy interface {
	EnsureRegistrationOpen(ctx context.Context, method string) error
	VerifyHuman(ctx context.Context, token, ip string) error
	TakeIPPermit(ctx context.Context, ip string) error
}

// DeviceLoginStore is the persistence surface used by device login and device
// registration. It excludes unrelated application repositories.
type DeviceLoginStore interface {
	User() repository.UserRepo
	UserAuth() repository.UserAuthRepo
	UserDevice() repository.UserDeviceRepo
	UserSubscription() repository.UserSubscriptionRepo
	UserCache() repository.UserCacheRepo
	Subscribe() repository.SubscribeRepo
	Log() repository.LogRepo
	InTx(ctx context.Context, fn func(repository.Store) error) error
}

// DeviceLoginConfig is the configuration snapshot consumed by the device
// login and registration use case.
type DeviceLoginConfig struct {
	Enabled           bool
	OnlyRealDevice    bool
	InviteForced      bool
	OnlyFirstPurchase bool
	TrialEnabled      bool
	TrialSubscribeID  int64
	TrialTime         int64
	TrialTimeUnit     string
	JWTAccessSecret   string
	JWTAccessExpire   int64
}

// DeviceLoginDependencies explicitly declares the collaborators required by
// device login instead of passing ServiceContext to business logic.
type DeviceLoginDependencies struct {
	Store  DeviceLoginStore
	Redis  *redis.Client
	Config DeviceLoginConfig
	Policy DeviceRegistrationPolicy
}
