// Package adminuser implements the admin-side account management subdomain
// of the identity module: user CRUD, auth methods, devices and login logs.
// Only the module facade may reach it.
package adminuser

import (
	"context"
	"os"
	"strings"

	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Users     repository.UserRepo
	UserAuths UserAuths
	Devices   repository.UserDeviceRepo
	Cache     repository.UserCacheRepo
	// Logs is the read port onto the platform domain's login logs.
	Logs repository.LogRepo
	// Wallet is the port onto the billing domain: the admin views show
	// wallet values from the authoritative table, not the legacy user
	// columns (ADR-001 step 5), and the money edits run as billing's own
	// transaction after the identity one.
	Wallet Wallets
	Store  Store
	Redis  *redis.Client
	// KickDevice force-disconnects a bound device.
	KickDevice func(userID int64, identifier string)
	// SubscriptionCaches and ServerCaches drop the subscription tokens and
	// node user lists that keep serving an account after its access ended
	// (the subscription and network facades).
	SubscriptionCaches SubscriptionCaches
	ServerCaches       ServerCaches
}

// UserAuths is the part of the identity bindings the admin flows read and
// change outside a transaction.
type UserAuths interface {
	// FindUserAuthMethods returns every binding of the account.
	FindUserAuthMethods(ctx context.Context, userID int64) ([]*user.AuthMethods, error)
	// FindUserAuthMethodByOpenID returns the binding of method whose
	// identifier is openID, the duplicate check of an account the
	// administrator creates.
	FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error)
	// FindUserAuthMethodByPlatform returns the account's binding of platform.
	FindUserAuthMethodByPlatform(ctx context.Context, userID int64, platform string) (*user.AuthMethods, error)
	UpdateUserAuthMethods(ctx context.Context, data *user.AuthMethods) error
	DeleteUserAuthMethods(ctx context.Context, userID int64, platform string) error
}

// Wallets is the billing port of the admin account flows; the billing facade
// provides it.
type Wallets interface {
	// FindWallet reads a user's wallet; a user without a wallet row reads as
	// nil. FindWallets reads several; users without a row are absent from
	// the map.
	FindWallet(ctx context.Context, userID int64) (*wallet.Wallet, error)
	FindWallets(ctx context.Context, userIDs []int64) (map[int64]*wallet.Wallet, error)
	// OpenWallet sets the opening amounts of an account the administrator
	// created.
	OpenWallet(ctx context.Context, opening wallet.Wallet) error
	// AdjustWallet sets the wallet amounts the adjustment carries, auditing
	// each change; amounts left nil or already equal are left alone.
	AdjustWallet(ctx context.Context, adjustment wallet.Adjustment) error
}

func (d Deps) kickDevice(userID int64, identifier string) {
	if d.KickDevice != nil {
		d.KickDevice(userID, identifier)
	}
}

// demoAdminID is the administrator account of the public demo instance.
const demoAdminID = 2

// demoMode reports whether this instance is the public demo, whose
// administrator must stay usable.
func demoMode() bool {
	return strings.EqualFold(os.Getenv("PPANEL_MODE"), "demo")
}

func demoRestricted(operation string) error {
	return xerr.Errorf(xerr.DemoModeRestricted, "demo mode does not allow to %s", operation)
}

// Service is the admin account management entry point used by the identity
// facade.
type Service struct {
	deps Deps
}

// NewService builds the subdomain over the dependencies the facade forwards.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.IdentityTransactor
}
