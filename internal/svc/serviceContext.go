package svc

import (
	"context"

	"github.com/perfect-panel/server/pkg/device"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/nodeMultiplier"
	"github.com/perfect-panel/server/pkg/orm"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// PluginReadySignaler 用于等待插件管理器就绪
type PluginReadySignaler interface {
	WaitReady(ctx context.Context) error
}

type ServiceContext struct {
	Redis        *redis.Client
	Config       config.Config
	Queue        *asynq.Client
	Inspector    *asynq.Inspector
	ExchangeRate float64
	GeoIP        *IPLocation
	Store        repository.Store

	//NodeCache   *cache.NodeCacheClient
	Restart               func() error
	TelegramBot           *tgbotapi.BotAPI
	NodeMultiplierManager *nodeMultiplier.Manager
	AuthLimiter           *limit.PeriodLimit
	DeviceManager         *device.DeviceManager
	PluginReady           PluginReadySignaler
	PluginMgr             any // *plugin.Manager, avoids import cycle
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
	srv := &ServiceContext{
		Redis:        rds,
		Config:       c,
		Queue:        NewAsynqClient(c),
		Inspector:    NewAsynqInspector(c),
		ExchangeRate: 0,
		GeoIP:        geoIP,
		Store:        store,
		//NodeCache:   cache.NewNodeCacheClient(rds),
		AuthLimiter: authLimiter,
	}
	srv.DeviceManager = NewDeviceManager(srv)
	return srv

}
