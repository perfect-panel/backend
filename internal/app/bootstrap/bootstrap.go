// Package bootstrap loads the runtime configuration at startup and reloads a
// subsystem's part of it when an administrator changes its settings: it runs
// the database migration, seeds the first administrator, provisions the node
// secret and publishes each subsystem's stored settings (site, node, email,
// device, invite, verification, subscription, registration, mobile, currency
// and Telegram) into the process's runtime snapshot.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/app/migration/schema"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Dependencies is the startup/reconfiguration boundary. It owns only mutable
// runtime configuration and the services needed to load or publish it.
type Dependencies struct {
	Config        func() config.Config
	UpdateRuntime func(func(*config.Runtime))
	// Settings is the platform kernel's system-settings table the loaders
	// read, and SettingsTx runs the node-secret provisioning in one platform
	// transaction; the composition root adapts the shared store to both.
	Settings                 Settings
	SettingsTx               SettingsTransactor
	ExchangeRate             *billing.CurrencyRateCache
	Notification             TelegramNotifications
	SetTelegramBot           func(*tgbot.Bot)
	SetNodeMultiplierManager func(*network.MultiplierManager)
	// LoginMethods and Administrators are the identity module's part: the
	// stored authentication-method settings the loaders publish, and the
	// administrator accounts Migrate seeds and WarnDefaultAdminPassword
	// checks. The identity facade provides both.
	LoginMethods   LoginMethods
	Administrators Administrators
}

// Settings is the system-settings table as the loaders read it: one read per
// settings category, the node multiplier setting, and the insert that seeds
// the multiplier setting on first start.
type Settings interface {
	GetSiteConfig(ctx context.Context) ([]*system.System, error)
	GetInviteConfig(ctx context.Context) ([]*system.System, error)
	GetRegisterConfig(ctx context.Context) ([]*system.System, error)
	GetSubscribeConfig(ctx context.Context) ([]*system.System, error)
	GetVerifyConfig(ctx context.Context) ([]*system.System, error)
	GetVerifyCodeConfig(ctx context.Context) ([]*system.System, error)
	GetNodeConfig(ctx context.Context) ([]*system.System, error)
	GetCurrencyConfig(ctx context.Context) ([]*system.System, error)
	FindNodeMultiplierConfig(ctx context.Context) (*system.System, error)
	Insert(ctx context.Context, data *system.System) error
}

// NodeSettings is the server-settings access of the node-secret
// provisioning inside its transaction.
type NodeSettings interface {
	GetNodeConfig(ctx context.Context) ([]*system.System, error)
	UpdateValueByCategoryKey(ctx context.Context, category, key, value string, valueType ...string) error
}

// SettingsTransactor runs fn in one platform transaction. Reads inside it go
// to the database rather than the settings cache.
type SettingsTransactor interface {
	InSettingsTx(ctx context.Context, fn func(NodeSettings) error) error
}

// TelegramNotifications is the notification module's side of the Telegram
// bot: it handles the updates the long-polling loop receives, publishes the
// bot's command menu and sets up the administrators' group.
type TelegramNotifications interface {
	HandleTelegramUpdate(ctx context.Context, update *models.Update)
	PublishTelegramCommands(ctx context.Context) error
	SetupTelegramGroup(ctx context.Context) error
}

// LoginMethods reads the stored configuration of an authentication method,
// such as "email" or "telegram".
type LoginMethods interface {
	FindLoginMethod(ctx context.Context, method string) (*auth.Auth, error)
}

// Administrators seeds the first administrator of a database without
// accounts and finds the administrators who sign in with a given password.
type Administrators interface {
	CreateInitialAdministrator(ctx context.Context, email, password string) (bool, error)
	FindAdministratorsWithPassword(ctx context.Context, password string) ([]*user.User, error)
}

func (d *Dependencies) currentConfig() config.Config {
	if d == nil || d.Config == nil {
		return config.Config{}
	}
	return d.Config()
}

func (d *Dependencies) updateRuntime(update func(*config.Runtime)) {
	if d != nil && d.UpdateRuntime != nil {
		d.UpdateRuntime(update)
	}
}

// Subsystem names a runtime configuration subsystem an administrator can
// reload. The values are the names the admin settings handlers pass through
// their reinitialize callback.
type Subsystem string

// The reloadable subsystems.
const (
	SubsystemSite      Subsystem = "site"
	SubsystemNode      Subsystem = "node"
	SubsystemEmail     Subsystem = "email"
	SubsystemDevice    Subsystem = "device"
	SubsystemInvite    Subsystem = "invite"
	SubsystemVerify    Subsystem = "verify"
	SubsystemSubscribe Subsystem = "subscribe"
	SubsystemRegister  Subsystem = "register"
	SubsystemMobile    Subsystem = "mobile"
	SubsystemCurrency  Subsystem = "currency"
	SubsystemTelegram  Subsystem = "telegram"
)

// ErrUnknownSubsystem is returned by Reload for a name no subsystem answers
// to, so a misspelt reload request fails loudly instead of doing nothing.
var ErrUnknownSubsystem = errors.New("unknown runtime subsystem")

// loaders maps every reloadable subsystem to its loader. Each loader publishes
// its configuration only after everything it needs was read, so a failed load
// leaves the previous configuration in place.
var loaders = map[Subsystem]func(context.Context, *Dependencies) error{
	SubsystemSite:      Site,
	SubsystemNode:      Node,
	SubsystemEmail:     Email,
	SubsystemDevice:    Device,
	SubsystemInvite:    Invite,
	SubsystemVerify:    Verify,
	SubsystemSubscribe: Subscribe,
	SubsystemRegister:  Register,
	SubsystemMobile:    Mobile,
	SubsystemCurrency:  Currency,
	SubsystemTelegram:  Telegram,
}

// startupOrder is the order Start loads the subsystems in. Node reads the
// secret NodeSecret provisions, so NodeSecret runs right before it.
var startupOrder = []Subsystem{
	SubsystemSite, SubsystemNode, SubsystemEmail, SubsystemDevice, SubsystemInvite, SubsystemVerify,
	SubsystemSubscribe, SubsystemRegister, SubsystemMobile, SubsystemCurrency, SubsystemTelegram,
}

// Start loads startup state in dependency order. Migration and node-secret
// provisioning must precede every node configuration read. The first failure
// stops startup and is returned, so the caller fails fast instead of serving
// with a partially loaded configuration.
func Start(ctx context.Context, deps *Dependencies) error {
	if err := Migrate(ctx, deps); err != nil {
		return err
	}
	WarnDefaultAdminPassword(ctx, deps)
	return loadSubsystems(ctx, deps, startupOrder)
}

func loadSubsystems(ctx context.Context, deps *Dependencies, order []Subsystem) error {
	for _, subsystem := range order {
		if subsystem == SubsystemNode {
			if err := NodeSecret(ctx, deps); err != nil {
				return wrapf(err, xerr.ERROR, "provision the node secret")
			}
		}
		if err := loaders[subsystem](ctx, deps); err != nil {
			return wrapf(err, xerr.ERROR, "load the %s configuration", subsystem)
		}
	}
	return nil
}

// ReloadAll refreshes every subsystem, in startup order, for a restart of the
// HTTP server: the routes are rebuilt on the reloaded settings. Like Reload
// it leaves migration and node-secret provisioning, startup work, out, and
// the first failure is returned with that subsystem's previous configuration
// kept.
func ReloadAll(ctx context.Context, deps *Dependencies) error {
	for _, subsystem := range startupOrder {
		if err := Reload(ctx, deps, subsystem); err != nil {
			return err
		}
	}
	return nil
}

// Reload refreshes the subsystem changed by an administrator. Startup-only
// migration and node-secret provisioning are deliberately excluded. A failure
// is logged and returned, and the subsystem keeps its previous configuration.
func Reload(ctx context.Context, deps *Dependencies, subsystem Subsystem) error {
	load, ok := loaders[subsystem]
	if !ok {
		logger.WithContext(ctx).Errorw("[Reload] unknown subsystem, nothing reloaded", logger.Field("subsystem", string(subsystem)))
		return fmt.Errorf("reload %q: %w", string(subsystem), ErrUnknownSubsystem)
	}
	if err := load(ctx, deps); err != nil {
		logger.WithContext(ctx).Errorw("[Reload] reload failed, keeping the previous configuration",
			logger.Field("subsystem", string(subsystem)), logger.Field("error", err.Error()))
		return wrapf(err, xerr.ERROR, "reload the %s configuration", subsystem)
	}
	return nil
}

// wrapf adds context to err with xerr.Wrapf and keeps the cause's text in the
// message: Wrapf prints the cause only of an error that already carries a
// code.
func wrapf(err error, code uint32, format string, args ...any) error {
	var coded *xerr.CodeError
	if err != nil && !errors.As(err, &coded) {
		format += ": %v"
		args = append(args, err)
	}
	return xerr.Wrapf(err, code, format, args...)
}

// migrateSchema applies the pending schema migrations; tests replace it.
var migrateSchema = schema.Up

// Migrate brings the database schema up to date and seeds the configured
// first administrator into a database that holds no account yet. The seed
// runs on every start, not only after a migration that changed the schema:
// identity's CreateInitialAdministrator does nothing once an account exists,
// and a seed that failed on the first start (the schema was current from then
// on) used to leave a panel without any administrator until someone fixed
// the database by hand.
func Migrate(ctx context.Context, deps *Dependencies) error {
	current := deps.currentConfig()
	mc := orm.Mysql{
		Config: current.DatabaseConfig(),
	}
	now := time.Now()
	switch err := migrateSchema(mc.Driver(), mc.MigrationDsn()); {
	case err == nil:
		logger.Info("[Migrate] Database change, took " + time.Since(now).String())
	case errors.Is(err, schema.NoChange):
		logger.Info("[Migrate] database not change")
	default:
		logger.Errorf("[Migrate] Up error: %v", err.Error())
		return wrapf(err, xerr.ERROR, "migrate the database")
	}
	return seedFirstAdministrator(ctx, deps, current.Administrator.Email, current.Administrator.Password)
}

// seedFirstAdministrator has identity create the configured administrator
// when the database holds no account yet, as after a fresh installation's
// first migration. Once an account exists, a password still configured seeds
// nothing and only sits in the file in clear, so it is flagged.
func seedFirstAdministrator(ctx context.Context, deps *Dependencies, email, configuredPassword string) error {
	adminPassword, generated := initialAdminPassword(configuredPassword)
	created, err := deps.Administrators.CreateInitialAdministrator(ctx, email, adminPassword)
	if err != nil {
		logger.Errorf("[Migrate] CreateAdminUser error: %v", xerr.Detail(err))
		return wrapf(err, xerr.DatabaseInsertError, "seed the first administrator")
	}
	if !created {
		if configuredPassword != "" {
			logger.Errorw("[Security] Administrator.Password is still set in the configuration file although the first administrator exists; "+
				"it seeds nothing any more and only sits in the file in clear: remove it",
				logger.Field("email", email))
		}
		return nil
	}
	if generated {
		// Printed once, outside the structured logger's redaction, so the
		// operator can sign in; it is not stored anywhere else. It is printed
		// only once the account exists, so it never names a password that
		// signs in nowhere.
		log.Printf("[Migrate] Created administrator %s with generated password %s; sign in and change it now", email, adminPassword)
	}
	logger.Info("[Migrate] Create admin user success")
	return nil
}

// defaultAdminPassword is the value older releases seeded the first
// administrator with when none was configured.
const defaultAdminPassword = "password"

// initialAdminPassword returns the configured password for the first
// administrator, or a generated one when none was configured, and whether it
// generated it. The documented Docker and environment-variable installs
// configure none, and falling back to the published default would open the
// panel to anyone.
func initialAdminPassword(configured string) (string, bool) {
	if configured != "" {
		return configured, false
	}
	return random.KeyNew(20, 1), true
}

// WarnDefaultAdminPassword flags administrators that still sign in with the
// password older releases seeded, which anyone can look up.
func WarnDefaultAdminPassword(ctx context.Context, deps *Dependencies) {
	admins, err := deps.Administrators.FindAdministratorsWithPassword(ctx, defaultAdminPassword)
	if err != nil {
		logger.Errorf("[Migrate] Query admin users error: %v", xerr.Detail(err))
		return
	}
	for _, admin := range admins {
		email := ""
		for _, method := range admin.AuthMethods {
			if method.AuthType == "email" {
				email = method.AuthIdentifier
			}
		}
		logger.Errorw("[Security] An administrator still uses the default password; change it immediately",
			logger.Field("user_id", admin.Id), logger.Field("email", email))
	}
}
