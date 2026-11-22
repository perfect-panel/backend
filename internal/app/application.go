// Package app is the composition root. NewApplication connects the database,
// Redis, the GeoIP database and the task queue, and assembles the shared
// store, the seven module facades and the event bus on them; NewServices
// returns the services the process runs: the HTTP server with its runtime
// bootstrap, the task worker and the scheduler. Only cmd imports this
// package, which internal/arch enforces.
package app

import (
	"context"
	"fmt"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/app/state"
	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/eventbus"
	"github.com/perfect-panel/backend/internal/infra/geoip"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/support"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/internal/transport/devicesocket"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/redis/go-redis/v9"

	"gorm.io/gorm"
)

// Application is the assembled process: the infrastructure connections, the
// runtime state and the seven module facades, from which the services the
// process runs are built.
type Application struct {
	// DB is the application's connection pool; the readiness endpoint
	// pings it. The modules reach the database through Store.
	DB           *gorm.DB
	Redis        *redis.Client
	Runtime      *state.State
	Queue        *taskqueue.Client
	Inspector    *asynq.Inspector
	ExchangeRate *billing.CurrencyRateCache
	// GeoIP is the open GeoIP database, nil when none is available: the
	// request metadata then carries no location, and Enrich is a no-op.
	GeoIP *geoip.IPLocation
	Store repository.Store

	// Domain modules (see docs/design/adr-001-modular-monolith.md). Application is
	// their composition root; handlers call the module facades.
	Support      support.Service
	Billing      billing.Service
	Platform     platform.Service
	Subscription subscription.Service
	Identity     identity.Service
	Network      network.Service
	Notification notification.Service
	EventBus     *eventbus.Bus
	// TrafficUsage books reported traffic against the subscriptions; the
	// node API and the traffic flush share the one instance.
	TrafficUsage subscription.TrafficUsage

	AuthLimiter   *ratelimit.PeriodLimit
	DeviceManager *devicesocket.DeviceManager
}

// NewApplication connects the database, Redis and the task queue the
// configuration names, opens the GeoIP database when one is available, and
// assembles the application on them. It panics when a connection fails:
// nothing can run without them. The GeoIP database is optional unless the
// configuration requires it.
func NewApplication(c config.Config) *Application {
	db, err := orm.ConnectMysql(orm.Mysql{
		Config: c.DatabaseConfig(),
	})
	if err != nil {
		panic(err.Error())
	}
	warnDatabaseTimeZone(c)

	geoIP := openGeoIP(c.GeoIP)

	rds := c.Redis.NewClient()
	if err := rds.Ping(context.Background()).Err(); err != nil {
		panic(err.Error())
	}
	return assemble(c, db, rds, geoIP, NewAsynqClient(c), NewAsynqInspector(c))
}

// openGeoIP opens the GeoIP databases the configuration names. Geolocation
// is best-effort metadata for the logs and audit records, so a database that
// is missing, fails its checksum or cannot be downloaded leaves the server
// without it, with the reason logged, unless GeoIP.Required makes that
// fatal. The server used to end when the download from the built-in mirror
// failed, which made an offline host unable to start.
func openGeoIP(cfg config.GeoIPConfig) *geoip.IPLocation {
	location, err := geoip.Open(geoip.Options{
		Path:        cfg.Path,
		Download:    cfg.Download,
		DownloadURL: cfg.DownloadURL,
		SHA256:      cfg.SHA256,
	})
	if err == nil {
		return location
	}
	if cfg.Required {
		logger.Errorf("[GeoIP] the database is required and unavailable: %v", err)
		panic(fmt.Sprintf("open the GeoIP database: %v", err))
	}
	logger.Errorw("[GeoIP] geolocation disabled: the database is unavailable, so requests and audit records carry no location; "+
		"place GeoLite2-City.mmdb at GeoIP.Path, or set GeoIP.Download and, for a mirror of your own, GeoIP.DownloadURL and GeoIP.SHA256",
		logger.Field("path", cfg.Path), logger.Field("error", err.Error()))
	return nil
}

// assemble builds the application on connected infrastructure: the store,
// the modules in their construction order and the event bus.
func assemble(c config.Config, db *gorm.DB, rds *redis.Client, geoIP *geoip.IPLocation, queue *taskqueue.Client, inspector *asynq.Inspector) *Application {
	authLimiter := ratelimit.NewPeriodLimit(86400, 15, rds, config.SendCountLimitKeyPrefix, ratelimit.Align())
	store := NewStore(db, rds)
	rate := billing.NewCurrencyRateCache(0)
	srv := &Application{
		DB:           db,
		Redis:        rds,
		Runtime:      state.New(c),
		Queue:        queue,
		Inspector:    inspector,
		ExchangeRate: rate,
		GeoIP:        geoIP,
		Store:        store,
		AuthLimiter:  authLimiter,
	}
	// Support takes srv for the ticket→Telegram mirror; the adapter reads
	// srv.Notification lazily, so constructing it before Notification is safe.
	srv.TrafficUsage = subscription.NewTrafficUsage(store)
	srv.Support = newSupportModule(store, queue, srv)
	srv.Billing = newBillingModule(c, store, queue, rds, rate, srv)
	srv.Platform = newPlatformModule(store, srv)
	srv.DeviceManager = NewDeviceManager(srv)
	srv.Subscription = newSubscriptionModule(store, srv)
	srv.Identity = newIdentityModule(store, srv)
	srv.Network = newNetworkModule(store, srv)
	srv.Notification = newNotificationModule(store, srv)
	srv.EventBus = newEventBus(store, srv)
	return srv
}
