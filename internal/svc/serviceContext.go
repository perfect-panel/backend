package svc

import (
	"context"

	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/exchangeRate"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/module/support"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/nodeMultiplier"
	"github.com/perfect-panel/server/pkg/orm"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type ServiceContext struct {
	Redis        *redis.Client
	Config       config.Config
	Queue        *asynq.Client
	Inspector    *asynq.Inspector
	ExchangeRate *exchangeRate.Cache
	GeoIP        *IPLocation
	Store        repository.Store

	// Domain modules (see docs/adr-001-modular-monolith.md). ServiceContext is
	// their composition root; handlers call the module facades.
	Support support.Service
	Billing billing.Service

	//NodeCache   *cache.NodeCacheClient
	Restart               func() error
	TelegramBot           *tgbotapi.BotAPI
	NodeMultiplierManager *nodeMultiplier.Manager
	AuthLimiter           *limit.PeriodLimit
	DeviceManager         *device.DeviceManager
}

func NewServiceContext(c config.Config) *ServiceContext {
	// gorm initialize
	db, err := orm.ConnectMysql(orm.Mysql{
		Config: c.DatabaseConfig(),
	})

	if err != nil {
		panic(err.Error())
	}

	// IP location initialize
	geoIP, err := NewIPLocation("./cache/GeoLite2-City.mmdb")
	if err != nil {
		panic(err.Error())
	}

	rds := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Host,
		Password: c.Redis.Pass,
		DB:       c.Redis.DB,
	})
	err = rds.Ping(context.Background()).Err()
	if err != nil {
		panic(err.Error())
	}
	authLimiter := limit.NewPeriodLimit(86400, 15, rds, config.SendCountLimitKeyPrefix, limit.Align())
	store := repository.NewGormStore(db, rds)
	queue := NewAsynqClient(c)
	rate := exchangeRate.NewCache(0)
	srv := &ServiceContext{
		Redis:        rds,
		Config:       c,
		Queue:        queue,
		Inspector:    NewAsynqInspector(c),
		ExchangeRate: rate,
		GeoIP:        geoIP,
		Store:        store,
		Support:      newSupportModule(store, queue),
		Billing:      newBillingModule(c, store, queue, rds, rate),
		//NodeCache:   cache.NewNodeCacheClient(rds),
		AuthLimiter: authLimiter,
	}
	srv.DeviceManager = NewDeviceManager(srv)
	return srv

}
