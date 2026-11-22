// Package scheduler enqueues the periodic tasks on their schedules, in the
// application's time zone. Every replica runs a scheduler; a tick's task id
// names its slot, so the replicas' enqueues of one slot collapse into one
// task.
package scheduler

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/robfig/cron/v3"
)

// slotRetentionMargin keeps a finished periodic task, and with it its slot's
// task ID, past the end of the slot, so a replica whose clock lags the one
// that enqueued the slot still collides with it.
const slotRetentionMargin = 5 * time.Minute

// periodicTask is one scheduled enqueue.
type periodicTask struct {
	spec     string
	taskType string
	name     string
	opts     []asynq.Option
}

var periodicTasks = []periodicTask{
	// schedule check subscription task: every 60 seconds. A failed sweep is
	// not retried: the next tick a minute later runs it afresh.
	{spec: "@every 60s", taskType: taskqueue.SchedulerCheckSubscription, name: "check subscription", opts: []asynq.Option{asynq.MaxRetry(0)}},
	// schedule aggregated traffic flush task: every 60 seconds
	{spec: "@every 60s", taskType: taskqueue.SchedulerFlushTraffic, name: "flush traffic", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// Paid order state doubles as a durable activation outbox. Reconcile it
	// periodically so a transient Redis outage cannot strand a paid order.
	{spec: "@every 60s", taskType: taskqueue.SchedulerReconcilePaidOrders, name: "paid order reconciliation", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// A one-shot close task can be lost before enqueue or exhaust retries. The
	// pending-state reconciler is the durable backstop for stock/coupon holds.
	{spec: "@every 60s", taskType: taskqueue.SchedulerReconcilePendingOrders, name: "pending order reconciliation", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// Drain the order-event outbox frequently enough for interactive checkout,
	// while retaining the database event record as the recovery path.
	{spec: "@every 5s", taskType: taskqueue.SchedulerPublishOrderEvents, name: "order event publisher", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// Drain the generic domain-event outbox: registration trials and future
	// cross-module events ride on it.
	{spec: "@every 5s", taskType: taskqueue.SchedulerDispatchDomainEvents, name: "domain event dispatcher", opts: []asynq.Option{asynq.MaxRetry(3)}},
	{spec: "0 3 * * *", taskType: taskqueue.SchedulerCleanupOrderEvents, name: "order event cleanup", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// schedule reset traffic task: every day at 00:30. asynq retries a failed
	// run (half an hour apart, see the task server); the run resets each
	// subscription at most once per day, so a retry finishes what failed.
	{spec: "30 0 * * *", taskType: taskqueue.SchedulerResetTraffic, name: "reset traffic", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// schedule pre-expiry reminder task: every day at 10:00. A reminder is
	// user-facing, so it goes out during the day rather than overnight, and
	// once daily rather than on the minute-by-minute lifecycle sweep.
	{spec: "0 10 * * *", taskType: taskqueue.SchedulerRemindExpiringSubscriptions, name: "expiring subscription reminder", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// schedule daily order report task: every day at 00:10, reporting the
	// day that just ended
	{spec: "10 0 * * *", taskType: taskqueue.SchedulerDailyOrderReport, name: "daily order report", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// schedule traffic stat task: every day at 00:00
	{spec: "0 0 * * *", taskType: taskqueue.SchedulerTrafficStat, name: "traffic stat", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// Retention runs independently after daily statistics. It retries failures
	// instead of waiting silently for the next day's statistics task.
	{spec: "30 2 * * *", taskType: taskqueue.SchedulerLogCleanup, name: "log cleanup", opts: []asynq.Option{asynq.MaxRetry(3)}},
	// schedule update exchange rate task: every day at 01:00
	{spec: "0 1 * * *", taskType: taskqueue.SchedulerExchangeRate, name: "update exchange rate", opts: []asynq.Option{asynq.MaxRetry(3)}},
}

// Service enqueues the periodic tasks. Every process runs one, so a tick is
// enqueued under a task ID naming the task type and the tick's slot: the
// replicas firing for the same slot collide on that ID (asynq keeps it while
// the task is pending, running, retrying or retained as completed) and only
// the first enqueue lands. asynq.Unique cannot do this: it releases its lock
// as soon as the task succeeds, so a replica ticking after that enqueues the
// slot again. A slot's task never blocks the next slot, however long it runs.
type Service struct {
	cron   *cron.Cron
	client *asynq.Client
	now    func() time.Time

	mu      sync.Mutex
	stopped bool
	done    chan struct{}
}

// NewService builds the scheduler on the queue's Redis connection; the
// composition root owns that connection's settings.
func NewService(redisOpt asynq.RedisConnOpt, appLocation string) *Service {
	location, err := time.LoadLocation(appLocation)
	if err != nil {
		logger.Errorf("load timezone location %q failed: %v, falling back to Local", appLocation, err)
		location = time.Local
	}
	return newService(redisOpt, location)
}

func newService(redisOpt asynq.RedisConnOpt, location *time.Location) *Service {
	return &Service{
		cron:   cron.New(cron.WithLocation(location)),
		client: asynq.NewClient(redisOpt),
		now:    time.Now,
		done:   make(chan struct{}),
	}
}

// Start registers the periodic tasks and blocks until Stop.
func (m *Service) Start() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	logger.Infof("start scheduler service")
	for _, task := range periodicTasks {
		if err := m.register(task); err != nil {
			logger.Errorf("register %s task failed: %s", task.name, err.Error())
		}
	}
	m.cron.Start()
	m.mu.Unlock()
	<-m.done
}

// Stop stops the schedule, waits for an enqueue in flight and closes the
// queue client.
func (m *Service) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	logger.Info("stop scheduler service")
	// Let an in-flight enqueue finish before its client closes.
	<-m.cron.Stop().Done()
	if err := m.client.Close(); err != nil {
		logger.Errorf("close scheduler client failed: %s", err.Error())
	}
	close(m.done)
}

func (m *Service) register(task periodicTask) error {
	schedule, err := cron.ParseStandard(task.spec)
	if err != nil {
		return err
	}
	m.cron.Schedule(schedule, cron.FuncJob(func() { m.enqueue(task, schedule) }))
	return nil
}

// enqueue enqueues the tick of task that fires now, once across replicas,
// and reports whether this replica's enqueue is the one that landed.
func (m *Service) enqueue(task periodicTask, schedule cron.Schedule) bool {
	slot, length := tickSlot(schedule, m.now())
	opts := append(append([]asynq.Option{}, task.opts...),
		asynq.TaskID(fmt.Sprintf("%s:%d", task.taskType, slot.Unix())),
		asynq.Retention(length+slotRetentionMargin),
	)
	_, err := m.client.Enqueue(asynq.NewTask(task.taskType, nil), opts...)
	// A conflict is the expected outcome on every replica but the first.
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		logger.Errorf("enqueue %s task failed: %s", task.name, err.Error())
	}
	return err == nil
}

// tickSlot returns the start and length of the slot a tick at now fires for.
// An @every schedule counts from each process's start, so replicas tick at
// different offsets: the slot is the interval-aligned window holding the
// tick. A cron spec fires at the same wall-clock minute on every replica, so
// the slot is that minute.
func tickSlot(schedule cron.Schedule, now time.Time) (time.Time, time.Duration) {
	if every, ok := schedule.(cron.ConstantDelaySchedule); ok {
		return now.Truncate(every.Delay), every.Delay
	}
	return now.Truncate(time.Minute), time.Minute
}
