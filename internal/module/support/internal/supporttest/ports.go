package supporttest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
)

// The fakes below stand in for the support module's ports onto the other
// domains. Each implements its port completely and records what the module
// asked of it; none is safe for concurrent use, as the flows under test call
// them from the request goroutine.

// Subscriptions is the subscription port the document rendering asks: the
// users in Active hold an active subscription, and Err fails every question.
type Subscriptions struct {
	Active map[int64]bool
	Err    error
	// Asked records the users asked about.
	Asked []int64
}

// Subscribed returns the port answering that exactly ids hold an active
// subscription.
func Subscribed(ids ...int64) *Subscriptions {
	active := make(map[int64]bool, len(ids))
	for _, id := range ids {
		active[id] = true
	}
	return &Subscriptions{Active: active}
}

// HasActiveSubscription reports whether userID is in Active.
func (s *Subscriptions) HasActiveSubscription(_ context.Context, userID int64) (bool, error) {
	s.Asked = append(s.Asked, userID)
	if s.Err != nil {
		return false, s.Err
	}
	return s.Active[userID], nil
}

// Recipients is the identity port selecting the recipients of an email
// campaign: every query answers Emails, and every count their number.
type Recipients struct {
	Emails []string
	Err    error
	// Queried and Counted record the filters of the queries and counts.
	Queried []user.EmailRecipientFilter
	Counted []user.EmailRecipientFilter
}

// QueryEmailRecipients answers Emails.
func (r *Recipients) QueryEmailRecipients(_ context.Context, filter *user.EmailRecipientFilter) ([]string, error) {
	r.Queried = append(r.Queried, *filter)
	if r.Err != nil {
		return nil, r.Err
	}
	return append([]string(nil), r.Emails...), nil
}

// CountEmailRecipients answers the number of Emails.
func (r *Recipients) CountEmailRecipients(_ context.Context, filter *user.EmailRecipientFilter) (int64, error) {
	r.Counted = append(r.Counted, *filter)
	if r.Err != nil {
		return 0, r.Err
	}
	return int64(len(r.Emails)), nil
}

// QuotaTargets is the subscription port selecting the user subscriptions a
// quota task applies to: every query answers IDs, and every count their
// number.
type QuotaTargets struct {
	IDs []int64
	Err error
	// Queried and Counted record the filters of the queries and counts.
	Queried []usersub.SubscribeFilter
	Counted []usersub.SubscribeFilter
}

// QuerySubscribeIdsByFilter answers IDs.
func (q *QuotaTargets) QuerySubscribeIdsByFilter(_ context.Context, filter *usersub.SubscribeFilter) ([]int64, error) {
	q.Queried = append(q.Queried, *filter)
	if q.Err != nil {
		return nil, q.Err
	}
	return append([]int64(nil), q.IDs...), nil
}

// CountSubscribesByFilter answers the number of IDs.
func (q *QuotaTargets) CountSubscribesByFilter(_ context.Context, filter *usersub.SubscribeFilter) (int64, error) {
	q.Counted = append(q.Counted, *filter)
	if q.Err != nil {
		return 0, q.Err
	}
	return int64(len(q.IDs)), nil
}

// QueuedEmail is a campaign handed to the queue and the time it is to run.
type QueuedEmail struct {
	TaskID    int64
	ProcessAt time.Time
}

// Queue is the marketing task queue. It records every task handed to it;
// Err refuses them all, and Received, when set, runs as a task arrives,
// before the queue answers (a worker picking the task up at once).
type Queue struct {
	Emails   []QueuedEmail
	Quotas   []int64
	Err      error
	Received func(taskID int64)
}

// EnqueueBatchEmail records the campaign.
func (q *Queue) EnqueueBatchEmail(_ context.Context, taskID int64, processAt time.Time) (string, error) {
	q.Emails = append(q.Emails, QueuedEmail{TaskID: taskID, ProcessAt: processAt})
	if q.Received != nil {
		q.Received(taskID)
	}
	if q.Err != nil {
		return "", q.Err
	}
	return fmt.Sprintf("marketing-email-%d", taskID), nil
}

// EnqueueQuota records the quota task.
func (q *Queue) EnqueueQuota(_ context.Context, taskID int64) error {
	q.Quotas = append(q.Quotas, taskID)
	if q.Received != nil {
		q.Received(taskID)
	}
	return q.Err
}

// Stopper records the campaigns whose running worker it was asked to stop.
type Stopper struct {
	Stopped []int64
}

// StopBatchEmail records the campaign.
func (s *Stopper) StopBatchEmail(taskID int64) {
	s.Stopped = append(s.Stopped, taskID)
}

// MirroredReply is a ticket reply mirrored to the admin group.
type MirroredReply struct {
	TicketID      int64
	From, Content string
}

// MirroredStatus is a ticket status change mirrored to the admin group.
type MirroredStatus struct {
	TicketID int64
	Status   uint8
}

// Notifier records the ticket lifecycle the module mirrors to the Telegram
// admin group.
type Notifier struct {
	Created  []int64
	Replies  []MirroredReply
	Statuses []MirroredStatus
}

// TicketCreated records the new ticket's id.
func (n *Notifier) TicketCreated(_ context.Context, t *ticket.Ticket) {
	n.Created = append(n.Created, t.Id)
}

// TicketReplied records the reply.
func (n *Notifier) TicketReplied(_ context.Context, ticketID int64, from, content string) {
	n.Replies = append(n.Replies, MirroredReply{TicketID: ticketID, From: from, Content: content})
}

// TicketStatusChanged records the status change.
func (n *Notifier) TicketStatusChanged(_ context.Context, ticketID int64, status uint8) {
	n.Statuses = append(n.Statuses, MirroredStatus{TicketID: ticketID, Status: status})
}

// AuditLog is the platform log port the administrators' mutations are
// recorded in: every row written, and Err failing every write.
type AuditLog struct {
	Rows []*log.SystemLog
	Err  error
}

// Insert records the row.
func (a *AuditLog) Insert(_ context.Context, row *log.SystemLog) error {
	if a.Err != nil {
		return a.Err
	}
	a.Rows = append(a.Rows, row)
	return nil
}

// Actions decodes the administrator actions recorded so far.
func (a *AuditLog) Actions(t testing.TB) []log.AdminAction {
	t.Helper()
	actions := make([]log.AdminAction, 0, len(a.Rows))
	for _, row := range a.Rows {
		if row.Type != log.TypeAdminAction.Uint8() {
			t.Fatalf("audit row %+v is not an administrator action", row)
		}
		var action log.AdminAction
		if err := action.Unmarshal([]byte(row.Content)); err != nil {
			t.Fatalf("audit row %d: %v", row.Id, err)
		}
		actions = append(actions, action)
	}
	return actions
}
