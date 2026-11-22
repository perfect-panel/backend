package task

import (
	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/eventbus"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/billing"
	moduleSubscription "github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/internal/transport/task/email"
	"github.com/perfect-panel/backend/internal/transport/task/events"
	"github.com/perfect-panel/backend/internal/transport/task/maintenance"
	"github.com/perfect-panel/backend/internal/transport/task/order"
	"github.com/perfect-panel/backend/internal/transport/task/sms"
	"github.com/perfect-panel/backend/internal/transport/task/subscription"
	"github.com/perfect-panel/backend/internal/transport/task/traffic"
)

// Dependencies are what the task handlers are built from; the composition
// root provides them.
type Dependencies struct {
	Email        email.Dependencies
	SMS          sms.Dependencies
	Order        order.Dependencies
	EventBus     *eventbus.Bus
	Traffic      traffic.Dependencies
	Subscription moduleSubscription.Service
	// Tasks and System are the platform kernel's scheduled-task bookkeeping
	// and system settings.
	Tasks        repository.TaskRepo
	System       repository.SystemRepo
	ExchangeRate *billing.CurrencyRateCache
	// Bootstrapped fires once the runtime settings the handlers read are
	// loaded; the worker consumes only after it.
	Bootstrapped Readiness
}

// RegisterHandlers binds every task type to its handler.
func RegisterHandlers(mux *asynq.ServeMux, deps Dependencies) {
	mux.Handle(taskqueue.ForthwithSendEmail, email.NewSendEmailHandler(deps.Email))
	mux.Handle(taskqueue.ForthwithSendSms, sms.NewSendSmsHandler(deps.SMS))
	mux.Handle(taskqueue.DeferCloseOrder, order.NewDeferCloseOrderHandler(deps.Order))
	mux.Handle(taskqueue.ForthwithActivateOrder, order.NewActivateOrderHandler(deps.Order.Billing))
	// Recover paid orders whose activation enqueue was interrupted.
	mux.Handle(taskqueue.SchedulerReconcilePaidOrders, order.NewReconcilePaidOrdersHandler(deps.Order))
	// Close stale pending orders even when their one-shot deferred task was
	// lost during a Redis outage or exhausted its retries.
	mux.Handle(taskqueue.SchedulerReconcilePendingOrders, order.NewReconcilePendingOrdersHandler(deps.Order))
	// Deliver durable order events to Redis Pub/Sub. The database remains the
	// source of truth for SSE replay when publication is delayed or duplicated.
	mux.Handle(taskqueue.SchedulerPublishOrderEvents, order.NewPublishOrderEventsHandler(deps.Order))
	// Domain events: the pump publishes outbox rows onto the queue; the
	// delivery worker runs the topic's subscribers per event.
	mux.Handle(taskqueue.SchedulerDispatchDomainEvents, events.NewDispatchDomainEventsHandler(deps.EventBus))
	mux.Handle(taskqueue.EventDeliver, events.NewDeliverDomainEventHandler(deps.EventBus))
	mux.Handle(taskqueue.SchedulerCleanupOrderEvents, order.NewCleanupOrderEventsHandler(deps.Order))
	// Daily settlement summary for administrators bound on Telegram.
	mux.Handle(taskqueue.SchedulerDailyOrderReport, order.NewDailyOrderReportHandler(deps.Order))

	mux.Handle(taskqueue.SchedulerFlushTraffic, traffic.NewFlushTrafficHandler(deps.Traffic))
	mux.Handle(taskqueue.SchedulerCheckSubscription, subscription.NewCheckSubscriptionHandler(deps.Subscription, deps.Traffic.Redis))
	// Warn owners before their subscription expires.
	mux.Handle(taskqueue.SchedulerRemindExpiringSubscriptions, subscription.NewRemindExpiringHandler(deps.Subscription))
	mux.Handle(taskqueue.SchedulerResetTraffic, traffic.NewResetTrafficHandler(deps.Subscription, deps.Traffic.Redis))
	mux.Handle(taskqueue.ScheduledBatchSendEmail, email.NewBatchEmailHandler(deps.Email))
	mux.Handle(taskqueue.SchedulerTrafficStat, traffic.NewStatHandler(deps.Traffic.Statistics))
	// The log cleanup is independent from traffic aggregation so either task
	// can retry without suppressing the other.
	mux.Handle(taskqueue.SchedulerLogCleanup, traffic.NewLogCleanupHandler(deps.Traffic.Logs))

	mux.Handle(taskqueue.ForthwithQuotaTask, maintenance.NewQuotaTaskHandler(deps.Subscription, deps.Tasks))
	mux.Handle(taskqueue.SchedulerExchangeRate, maintenance.NewRateHandler(maintenance.RateDependencies{System: deps.System, ExchangeRate: deps.ExchangeRate}))
}
