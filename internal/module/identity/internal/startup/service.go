// Package startup implements the identity module's part of the server start:
// seeding the first administrator of a fresh database, finding the
// administrators still signing in with a published default password, and the
// check and data fix-up that run once the schema is current. Only the module
// facade may reach it.
package startup

import (
	"github.com/perfect-panel/backend/internal/repository"
)

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Users     repository.UserRepo
	UserAuths repository.UserAuthRepo
	// Auths lists the configured sign-in methods, for the start-up check of
	// the methods whose redirects need a site host; optional.
	Auths repository.AuthRepo
	// SiteHost snapshots the configured site host; optional.
	SiteHost func() string
	// Store carries the identity transaction that counts the accounts and
	// creates the first administrator together.
	Store Store
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.IdentityTransactor
}

// Service is the startup subdomain entry point used by the identity facade.
type Service struct {
	deps Deps
}

// NewService builds the subdomain over the module's own repositories.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
