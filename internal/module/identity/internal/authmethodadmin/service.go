// Package authmethodadmin implements the admin-side authentication-method
// subdomain of the identity module: method configuration, sender platforms
// and test sends. Only the module facade may reach it.
package authmethodadmin

import (
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Snapshot is the per-request view of the runtime-mutable sender platform
// settings.
type Snapshot struct {
	EmailPlatform        string
	EmailPlatformConfig  string
	MobilePlatform       string
	MobilePlatformConfig string
	SiteName             string
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Auths repository.AuthRepo
	// Config snapshots the runtime-mutable sender settings per request.
	Config func() Snapshot
	// Reinitialize re-runs a runtime subsystem's initialization after its
	// configuration changed ("email", "mobile", "device" or "telegram").
	Reinitialize func(subsystem string) error
}

// Service is the auth-method administration entry point used by the identity
// facade.
type Service struct {
	deps Deps
}

// NewService builds the subdomain over the dependencies the facade forwards.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// methodConfig is the admin view of a stored method, its configuration
// decoded from the stored JSON. A method without a stored configuration
// shows it as the empty string.
func methodConfig(method *auth.Auth) (*dto.AuthMethodConfig, error) {
	view := &dto.AuthMethodConfig{
		Id:      method.Id,
		Method:  method.Method,
		Config:  method.Config,
		Enabled: method.Enabled != nil && *method.Enabled,
	}
	if method.Config != "" {
		if err := json.Unmarshal([]byte(method.Config), &view.Config); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "decode stored %s config", method.Method)
		}
	}
	return view, nil
}
