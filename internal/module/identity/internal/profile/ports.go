// Package profile implements the self-service identity subdomain of the
// identity module: account info, credentials, third-party bindings, devices
// and notification preferences. Only the module facade may reach it.
package profile

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthflow"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Users     Users
	UserAuth  UserAuths
	Auth      repository.AuthRepo
	Devices   repository.UserDeviceRepo
	UserCache repository.UserCacheRepo
	Logs      repository.LogRepo
	// Wallet is the billing-domain read port for the account view.
	Wallet WalletReader
	Redis  *redis.Client
	// Store carries the identity-scoped transaction for device unbinding.
	Store Store
	// Policy gates method rebinding on the same switches as sign-in.
	Policy registerpolicy.Policy
	// OAuth runs the provider round trip of account binding, shared with
	// sign-in.
	OAuth *oauthflow.Flow

	// EmailDomains snapshots the runtime-mutable email domain-suffix policy.
	EmailDomains func() (domainList string, restrict bool)
	// TelegramBotName snapshots the runtime-mutable Telegram bot name.
	TelegramBotName func() string
	// NotifyUnbind sends the best-effort Telegram unbind notice through the
	// runtime-configured bot.
	NotifyUnbind func(ctx context.Context, userID, chatID int64) error
	// NotifyPasswordChanged tells the account, best effort, that its
	// password changed and which third-party sign-in methods stay bound;
	// optional.
	NotifyPasswordChanged func(ctx context.Context, userID int64, bindings []string) error
	// KickDevice force-disconnects a device once its binding is removed.
	KickDevice func(userID int64, identifier string)
}

// Users is the part of the account rows the profile flows write: the
// notification switches, the rules and the password of the calling account.
type Users interface {
	// UpdateColumns writes only the named columns of the account.
	UpdateColumns(ctx context.Context, id int64, columns map[string]any) error
}

// UserAuths is the part of the identity bindings the profile flows read and
// change: the calling account's email, mobile and third-party bindings.
type UserAuths interface {
	// FindUserAuthMethods returns every binding of the account.
	FindUserAuthMethods(ctx context.Context, userID int64) ([]*user.AuthMethods, error)
	// FindUserAuthMethodByOpenID returns the binding of method whose
	// identifier is openID, whichever account holds it.
	FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error)
	// FindUserAuthMethodByUserId and FindUserAuthMethodByPlatform return the
	// account's binding of a method.
	FindUserAuthMethodByUserId(ctx context.Context, method string, userID int64) (*user.AuthMethods, error)
	FindUserAuthMethodByPlatform(ctx context.Context, userID int64, platform string) (*user.AuthMethods, error)
	InsertUserAuthMethods(ctx context.Context, data *user.AuthMethods) error
	UpdateUserAuthMethods(ctx context.Context, data *user.AuthMethods) error
	DeleteUserAuthMethods(ctx context.Context, userID int64, platform string) error
}

// WalletReader reads a user's wallet from the billing module; a user without
// a wallet row reads as nil.
type WalletReader interface {
	FindWallet(ctx context.Context, userID int64) (*wallet.Wallet, error)
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.IdentityTransactor
}
