// Package oauth implements OAuth sign-in: the authorization URL, the
// callback that signs an existing account in or registers a new one, and
// Apple's form-post callback. The provider round trip is oauthflow's, shared
// with account binding.
package oauth

import (
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthflow"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Config is the per-request view of the settings OAuth sign-in consumes.
type Config struct {
	InviteForced            bool
	OnlyFirstPurchase       bool
	EmailDomainSuffixList   string
	EmailEnableDomainSuffix bool
	Sessions                account.SessionConfig
	// SiteHost is the fallback redirect of the Apple form-post callback.
	SiteHost string
}

// Store is the persistence surface OAuth sign-in and registration use.
type Store interface {
	User() repository.UserRepo
	UserAuth() repository.UserAuthRepo
	Log() repository.LogRepo
	repository.IdentityTransactor
}

// Deps declares the collaborators of OAuth sign-in.
type Deps struct {
	Store  Store
	Redis  *redis.Client
	Policy registerpolicy.Policy
	Flow   *oauthflow.Flow
	// Config snapshots the runtime-mutable settings per request.
	Config func() Config
}

// Service signs users in through OAuth providers.
type Service struct {
	deps Deps
}

// NewService builds OAuth sign-in over its collaborators.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
