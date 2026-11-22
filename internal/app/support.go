package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/internal/module/support"
	ticket "github.com/perfect-panel/server/internal/module/support/entity/ticket"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/transport/task/email"
	"github.com/perfect-panel/server/pkg/logger"
)

// newSupportModule wires the support module against the application store.
// The adapters below bridge its remaining ports to the task queue, the email
// worker manager, the Telegram ticket topics and the identity and
// subscription facades.
func newSupportModule(store repository.Store, queue *taskqueue.Client, srv *Application) support.Service {
	return support.New(support.Deps{
		Announcements: store.Announcement(),
		Ads:           store.Ads(),
		Documents:     store.Document(),
		Tickets:       store.Ticket(),
		Tasks:         store.Task(),
		Subscriptions: supportSubscriptions{srv: srv},
		Recipients:    supportAccounts{srv: srv},
		QuotaTargets:  supportSubscriptions{srv: srv},
		Queue:         marketingQueue{client: queue},
		EmailStopper:  emailWorkerStopper{},
		TicketNotify:  ticketTopicNotifier{srv: srv, pool: newMirrorPool(mirrorWorkers, mirrorQueueSize)},
		Redis:         srv.Redis,
		AuditLogs:     store.Log(),
	})
}

// The mirror pool: a few workers relay ticket events into the Telegram
// group, and a bounded queue holds the events waiting for one. Telegram
// allows a bot about twenty messages a minute in a group, so more workers
// would only be throttled; a flood of replies fills the queue and the
// excess is dropped with a log line rather than kept as a goroutine each.
const (
	mirrorWorkers   = 4
	mirrorQueueSize = 256
	mirrorTimeout   = 15 * time.Second
)

// mirrorJob is one ticket event to relay.
type mirrorJob struct {
	ctx      context.Context
	ticketID int64
	what     string
	call     func(ctx context.Context) error
}

// mirrorPool runs the mirror jobs on a fixed number of workers.
type mirrorPool struct {
	jobs    chan mirrorJob
	start   sync.Once
	workers int
	// dropped counts the events the full queue turned away.
	dropped atomic.Int64
}

func newMirrorPool(workers, queueSize int) *mirrorPool {
	return &mirrorPool{jobs: make(chan mirrorJob, queueSize), workers: workers}
}

// submit queues job, starting the workers on first use, and reports whether
// it was accepted: a full queue turns the event away.
func (p *mirrorPool) submit(job mirrorJob) bool {
	p.start.Do(func() {
		for range p.workers {
			go p.run()
		}
	})
	select {
	case p.jobs <- job:
		return true
	default:
		p.dropped.Add(1)
		return false
	}
}

func (p *mirrorPool) run() {
	for job := range p.jobs {
		mirrorCtx, cancel := context.WithTimeout(job.ctx, mirrorTimeout)
		if err := job.call(mirrorCtx); err != nil {
			logger.WithContext(mirrorCtx).Errorw("[TicketTopic] "+job.what+" mirror failed",
				logger.Field("error", err.Error()), logger.Field("ticket_id", job.ticketID))
		}
		cancel()
	}
}

// ticketTopicNotifier mirrors ticket lifecycle into the Telegram admin
// group. Best-effort by the port's contract: the group being unconfigured
// or unreachable only logs — the ticket operation already succeeded. The
// mirror runs detached from the request, on the pool: a user submitting a
// ticket must not wait on Telegram round-trips (the bot client's HTTP
// timeout is 60s), and a flood of replies must not start a goroutine each.
type ticketTopicNotifier struct {
	srv  *Application
	pool *mirrorPool
}

func (n ticketTopicNotifier) enabled() bool {
	return n.srv.Runtime.Config().Telegram.GroupChatID != 0 && n.srv.Notification != nil
}

func (n ticketTopicNotifier) mirror(ctx context.Context, ticketID int64, what string, call func(ctx context.Context) error) {
	if !n.enabled() {
		return
	}
	job := mirrorJob{ctx: context.WithoutCancel(ctx), ticketID: ticketID, what: what, call: call}
	if !n.pool.submit(job) {
		logger.WithContext(ctx).Errorw("[TicketTopic] "+what+" mirror dropped: the mirror queue is full",
			logger.Field("ticket_id", ticketID))
	}
}

func (n ticketTopicNotifier) TicketCreated(ctx context.Context, t *ticket.Ticket) {
	n.mirror(ctx, t.Id, "create", func(ctx context.Context) error {
		return n.srv.Notification.NotifyTicketCreated(ctx, t)
	})
}

func (n ticketTopicNotifier) TicketReplied(ctx context.Context, ticketID int64, from, content string) {
	n.mirror(ctx, ticketID, "reply", func(ctx context.Context) error {
		return n.srv.Notification.NotifyTicketReplied(ctx, ticketID, from, content)
	})
}

func (n ticketTopicNotifier) TicketStatusChanged(ctx context.Context, ticketID int64, status uint8) {
	n.mirror(ctx, ticketID, "status", func(ctx context.Context) error {
		return n.srv.Notification.NotifyTicketStatusChanged(ctx, ticketID, status)
	})
}

// marketingQueue adapts the asynq client to the support module's
// MarketingQueue port, keeping queue task types out of the module.
type marketingQueue struct {
	client *taskqueue.Client
}

func (q marketingQueue) EnqueueBatchEmail(ctx context.Context, taskID int64, processAt time.Time) (string, error) {
	queueTaskID := fmt.Sprintf("marketing-email-%d-initial", taskID)
	t := asynq.NewTask(taskqueue.ScheduledBatchSendEmail, []byte(strconv.FormatInt(taskID, 10)))
	// A campaign run is paced over hours; the explicit timeout replaces
	// asynq's 30-minute default, and the worker continues from a follow-up
	// task before it runs out.
	if err := q.enqueueIdempotent(ctx, t, queueTaskID, asynq.ProcessAt(processAt), asynq.Timeout(taskqueue.BatchEmailTaskTimeout)); err != nil {
		return "", err
	}
	return queueTaskID, nil
}

func (q marketingQueue) EnqueueQuota(ctx context.Context, taskID int64) error {
	t := asynq.NewTask(taskqueue.ForthwithQuotaTask, []byte(strconv.FormatInt(taskID, 10)))
	return q.enqueueIdempotent(ctx, t, fmt.Sprintf("marketing-quota-%d", taskID))
}

// enqueueIdempotent retries once with the same task ID on a detached bounded
// context. This resolves the common "Redis accepted the write but the client
// lost the response" case as an ID conflict instead of falsely abandoning a
// durable database task.
func (q marketingQueue) enqueueIdempotent(ctx context.Context, t *asynq.Task, taskID string, opts ...asynq.Option) error {
	options := append(append([]asynq.Option{}, opts...), asynq.TaskID(taskID))
	_, err := q.client.EnqueueContext(ctx, t, options...)
	if err == nil || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}

	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, retryErr := q.client.EnqueueContext(retryCtx, t, options...)
	if retryErr == nil || errors.Is(retryErr, asynq.ErrTaskIDConflict) {
		return nil
	}
	return errors.Join(err, retryErr)
}

// emailWorkerStopper adapts the global batch-email worker manager to the
// support module's BatchEmailStopper port.
type emailWorkerStopper struct{}

func (emailWorkerStopper) StopBatchEmail(taskID int64) {
	if email.Manager == nil {
		logger.Error("[StopBatchSendEmail] email worker manager is nil, cannot stop task")
		return
	}
	email.Manager.RemoveWorker(taskID)
}

// supportSubscriptions serves support's subscription reads, the ticket
// flows' active-subscription check and the quota-task target selection, from
// the subscription facade, which is constructed after support and resolved
// per call.
type supportSubscriptions struct{ srv *Application }

func (s supportSubscriptions) HasActiveSubscription(ctx context.Context, userID int64) (bool, error) {
	subs, err := s.srv.Subscription.UserSubscriptions(ctx, userID, int64(usersub.SubscribeStatusActive))
	if err != nil {
		return false, err
	}
	return len(subs) > 0, nil
}

func (s supportSubscriptions) QuerySubscribeIdsByFilter(ctx context.Context, filter *usersub.SubscribeFilter) ([]int64, error) {
	return s.srv.Subscription.SelectSubscriptionIDs(ctx, filter)
}

func (s supportSubscriptions) CountSubscribesByFilter(ctx context.Context, filter *usersub.SubscribeFilter) (int64, error) {
	return s.srv.Subscription.CountSelectedSubscriptions(ctx, filter)
}

// supportAccounts serves the marketing emails' recipient selection from the
// identity facade, which is constructed after support and resolved per call.
type supportAccounts struct{ srv *Application }

func (a supportAccounts) QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error) {
	return a.srv.Identity.QueryEmailRecipients(ctx, filter)
}

func (a supportAccounts) CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error) {
	return a.srv.Identity.CountEmailRecipients(ctx, filter)
}
