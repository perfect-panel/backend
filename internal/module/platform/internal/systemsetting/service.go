// Package systemsetting implements the system configuration subdomain of the
// platform module. Runtime subsystem re-initialization, restart and the
// mutable configuration snapshot are reached through injected callbacks.
package systemsetting

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// MultiplierFunc evaluates the current node traffic multiplier.
type MultiplierFunc = func(at time.Time) float32

// SettingsWriter is what a settings update writes inside its transaction:
// the stored value of each setting.
type SettingsWriter interface {
	UpdateValueByCategoryKey(ctx context.Context, category, key, value string, valueType ...string) error
}

// AuditWriter records the administrator's change next to the settings it
// changed, in the same transaction.
type AuditWriter interface {
	Insert(ctx context.Context, data *log.SystemLog) error
}

// SettingsStore is what one settings update reaches inside its transaction:
// the settings it writes and the audit trail it records the change in.
type SettingsStore interface {
	SettingsWriter
	AuditWriter
}

// SettingsTransactor runs the writes of one settings update in a single
// transaction, so an update and its audit row are stored whole or not at
// all.
type SettingsTransactor interface {
	InSettingsTx(ctx context.Context, fn func(SettingsStore) error) error
}

// PlatformTransactor mirrors the store's platform-scoped transaction.
type PlatformTransactor interface {
	InPlatformTx(ctx context.Context, fn func(kernel.PlatformStore) error) error
}

// NewSettingsTransactor runs the settings writes in platform-scoped
// transactions of store, on its system settings and system log
// repositories.
func NewSettingsTransactor(store PlatformTransactor) SettingsTransactor {
	return platformSettings{store: store}
}

// platformSettings is a SettingsTransactor over the store's platform-scoped
// transaction.
type platformSettings struct {
	store PlatformTransactor
}

// settingsStore is the settings and the log of one platform transaction.
type settingsStore struct {
	SettingsWriter
	AuditWriter
}

func (p platformSettings) InSettingsTx(ctx context.Context, fn func(SettingsStore) error) error {
	return p.store.InPlatformTx(ctx, func(store kernel.PlatformStore) error {
		return fn(settingsStore{SettingsWriter: store.System(), AuditWriter: store.Log()})
	})
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	System kernel.SystemRepo
	// Store runs the writes of each update in one transaction.
	Store SettingsTransactor
	// Reinitialize re-runs a subsystem's initialization after its
	// configuration changed, reporting a failure.
	Reinitialize func(subsystem string) error
	// Restart restarts the transport server (subscribe path changes).
	Restart func() error
	// SubscribePath reads the currently active subscribe path.
	SubscribePath func() string
	// Multiplier evaluates the current node traffic multiplier.
	Multiplier func(at time.Time) float32
}

// reinit applies a subsystem's changed settings to the running server. A
// failure leaves the settings saved but not in effect until a reload
// succeeds or the server restarts, which the administrator has to learn:
// it is reported under a code of its own, since an internal error would
// read as the settings not having been saved.
func (d Deps) reinit(subsystem string) error {
	if d.Reinitialize == nil {
		return nil
	}
	if err := d.Reinitialize(subsystem); err != nil {
		return xerr.Wrapf(err, xerr.SettingsSavedNotApplied, "the %s settings are saved but could not be applied", subsystem)
	}
	return nil
}

func (d Deps) restart() error {
	if d.Restart == nil {
		return nil
	}
	return d.Restart()
}

func (d Deps) subscribePath() string {
	if d.SubscribePath == nil {
		return ""
	}
	return d.SubscribePath()
}

func (d Deps) multiplier(at time.Time) float32 {
	if d.Multiplier == nil {
		return 1
	}
	return d.Multiplier(at)
}

// Service reads and updates the system settings for the platform facade.
type Service struct {
	deps Deps
}

// NewService builds the system settings service.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
