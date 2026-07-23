package scheduler

import (
	"time"

	"github.com/perfect-panel/server/pkg/logger"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
)

type Service struct {
	svc    *svc.ServiceContext
	server *asynq.Scheduler
}

func NewService(svc *svc.ServiceContext) *Service {
	return &Service{
		svc:    svc,
		server: initService(svc),
	}
}

func (m *Service) Start() {
	logger.Infof("start scheduler service")
	// schedule check subscription task: every 60 seconds
	checkTask := asynq.NewTask(types.SchedulerCheckSubscription, nil)
	if _, err := m.server.Register("@every 60s", checkTask); err != nil {
		logger.Errorf("register check subscription task failed: %s", err.Error())
	}
	// schedule aggregated traffic flush task: every 60 seconds
	flushTrafficTask := asynq.NewTask(types.SchedulerFlushTraffic, nil)
	if _, err := m.server.Register("@every 60s", flushTrafficTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register flush traffic task failed: %s", err.Error())
	}
	// Paid order state doubles as a durable activation outbox. Reconcile it
	// periodically so a transient Redis outage cannot strand a paid order.
	reconcilePaidOrdersTask := asynq.NewTask(types.SchedulerReconcilePaidOrders, nil)
	if _, err := m.server.Register("@every 60s", reconcilePaidOrdersTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register paid order reconciliation task failed: %s", err.Error())
	}
	// A one-shot close task can be lost before enqueue or exhaust retries. The
	// pending-state reconciler is the durable backstop for stock/coupon holds.
	reconcilePendingOrdersTask := asynq.NewTask(types.SchedulerReconcilePendingOrders, nil)
	if _, err := m.server.Register("@every 60s", reconcilePendingOrdersTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register pending order reconciliation task failed: %s", err.Error())
	}
	// Drain the order-event outbox frequently enough for interactive checkout,
	// while retaining the database event record as the recovery path.
	publishOrderEventsTask := asynq.NewTask(types.SchedulerPublishOrderEvents, nil)
	if _, err := m.server.Register("@every 5s", publishOrderEventsTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register order event publisher task failed: %s", err.Error())
	}
	cleanupOrderEventsTask := asynq.NewTask(types.SchedulerCleanupOrderEvents, nil)
	if _, err := m.server.Register("0 3 * * *", cleanupOrderEventsTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register order event cleanup task failed: %s", err.Error())
	}
	//// schedule total server data task: every 5 minutes
	//totalServerDataTask := asynq.NewTask(types.SchedulerTotalServerData, nil)
	//if _, err := m.server.Register("@every 180s", totalServerDataTask); err != nil {
	//	logger.Errorf("register total server data task failed: %s", err.Error())
	//}
	// schedule reset traffic task: every day at 00:30
	resetTrafficTask := asynq.NewTask(types.SchedulerResetTraffic, nil)
	if _, err := m.server.Register("30 0 * * *", resetTrafficTask); err != nil {
		logger.Errorf("register reset traffic task failed: %s", err.Error())
	}

	// schedule traffic stat task: every day at 00:00
	trafficStatTask := asynq.NewTask(types.SchedulerTrafficStat, nil)
	if _, err := m.server.Register("0 0 * * *", trafficStatTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register traffic stat task failed: %s", err.Error())
	}

	// schedule update exchange rate task: every day at 01:00
	rateTask := asynq.NewTask(types.SchedulerExchangeRate, nil)
	if _, err := m.server.Register("0 1 * * *", rateTask, asynq.MaxRetry(3)); err != nil {
		logger.Errorf("register update exchange rate task failed: %s", err.Error())
	}

	if err := m.server.Run(); err != nil {
		logger.Errorf("run scheduler failed: %s", err.Error())
	}
}

func (m *Service) Stop() {
	logger.Info("stop scheduler service")
	m.server.Shutdown()
}

func initService(svc *svc.ServiceContext) *asynq.Scheduler {
	location, err := time.LoadLocation(svc.Config.AppLocation)
	if err != nil {
		logger.Errorf("load timezone location %q failed: %v, falling back to Local", svc.Config.AppLocation, err)
		location = time.Local
	}
	return asynq.NewScheduler(
		asynq.RedisClientOpt{Addr: svc.Config.Redis.Host, Password: svc.Config.Redis.Pass, DB: 5},
		&asynq.SchedulerOpts{
			Location: location,
		},
	)
}
