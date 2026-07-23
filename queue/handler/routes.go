package handler

import (
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	orderLogic "github.com/perfect-panel/server/queue/logic/order"
	smslogic "github.com/perfect-panel/server/queue/logic/sms"
	"github.com/perfect-panel/server/queue/logic/subscription"
	"github.com/perfect-panel/server/queue/logic/task"
	"github.com/perfect-panel/server/queue/logic/traffic"
	"github.com/perfect-panel/server/queue/types"

	emailLogic "github.com/perfect-panel/server/queue/logic/email"
)

func RegisterHandlers(mux *asynq.ServeMux, serverCtx *svc.ServiceContext) {
	// Send email task
	mux.Handle(types.ForthwithSendEmail, emailLogic.NewSendEmailLogic(serverCtx))
	// Send sms task
	mux.Handle(types.ForthwithSendSms, smslogic.NewSendSmsLogic(serverCtx))
	// Defer close order task
	mux.Handle(types.DeferCloseOrder, orderLogic.NewDeferCloseOrderLogic(serverCtx))
	// Forthwith activate order task
	mux.Handle(types.ForthwithActivateOrder, orderLogic.NewActivateOrderLogic(serverCtx))
	// Recover paid orders whose activation enqueue was interrupted.
	mux.Handle(types.SchedulerReconcilePaidOrders, orderLogic.NewReconcilePaidOrdersLogic(serverCtx))
	// Close stale pending orders even when their one-shot deferred task was
	// lost during a Redis outage or exhausted its retries.
	mux.Handle(types.SchedulerReconcilePendingOrders, orderLogic.NewReconcilePendingOrdersLogic(serverCtx))
	// Deliver durable order events to Redis Pub/Sub. The database remains the
	// source of truth for SSE replay when publication is delayed or duplicated.
	mux.Handle(types.SchedulerPublishOrderEvents, orderLogic.NewPublishOrderEventsLogic(serverCtx))
	mux.Handle(types.SchedulerCleanupOrderEvents, orderLogic.NewCleanupOrderEventsLogic(serverCtx))

	// Forthwith traffic statistics
	mux.Handle(types.ForthwithTrafficStatistics, traffic.NewTrafficStatisticsLogic(serverCtx))
	// Flush aggregated traffic
	mux.Handle(types.SchedulerFlushTraffic, traffic.NewFlushTrafficLogic(serverCtx))

	// Schedule check subscription
	mux.Handle(types.SchedulerCheckSubscription, subscription.NewCheckSubscriptionLogic(serverCtx))

	// Schedule total server data
	mux.Handle(types.SchedulerTotalServerData, traffic.NewServerDataLogic(serverCtx))

	// Schedule reset traffic
	mux.Handle(types.SchedulerResetTraffic, traffic.NewResetTrafficLogic(serverCtx))

	// ScheduledBatchSendEmail
	mux.Handle(types.ScheduledBatchSendEmail, emailLogic.NewBatchEmailLogic(serverCtx))

	// ScheduledTrafficStat
	mux.Handle(types.SchedulerTrafficStat, traffic.NewStatLogic(serverCtx))

	// ForthwithQuotaTask
	mux.Handle(types.ForthwithQuotaTask, task.NewQuotaTaskLogic(serverCtx))
	// SchedulerExchangeRate
	mux.Handle(types.SchedulerExchangeRate, task.NewRateLogic(serverCtx))
}
