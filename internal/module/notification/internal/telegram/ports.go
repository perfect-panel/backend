package telegram

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
)

// The ports below are the bot's own view of the domains it serves: declared
// here by the consumer and limited to the calls the bot makes. The
// composition root backs them with the owning domains' repositories, or with
// the owning module's facade where one serves the call, so each port can move
// to a facade without the bot changing. A missing record is reported as an
// error matching gorm.ErrRecordNotFound.

// Accounts is the bot's port onto the identity domain.
type Accounts interface {
	// FindUser returns the account, soft-deleted ones included.
	FindUser(ctx context.Context, id int64) (*user.User, error)
	// FindBinding resolves the auth method of authType whose identifier is
	// identifier, such as a Telegram chat id or an email address.
	FindBinding(ctx context.Context, authType, identifier string) (*user.AuthMethods, error)
	// FindUserBinding returns the user's auth method of authType.
	FindUserBinding(ctx context.Context, userID int64, authType string) (*user.AuthMethods, error)
	// ListBindings returns every auth method of the user.
	ListBindings(ctx context.Context, userID int64) ([]*user.AuthMethods, error)
	// BindTelegram records chatID as the user's verified Telegram binding.
	BindTelegram(ctx context.Context, userID int64, chatID string) error
	// SetEnabled enables or disables the account.
	SetEnabled(ctx context.Context, userID int64, enabled bool) error
	// CountRegistrations counts the accounts registered on the day of t.
	CountRegistrations(ctx context.Context, t time.Time) (int64, error)
}

// Tickets is the bot's port onto the support domain. Reply and SetStatus run
// the support module's ticket use case, so the change is mirrored into the
// ticket's topic like any other, unless inTopic says it was made there.
type Tickets interface {
	CountAwaitingReply(ctx context.Context) (int64, error)
	List(ctx context.Context, page, size int, status *uint8) (total int64, list []*ticket.Ticket, err error)
	Find(ctx context.Context, id int64) (*ticket.Ticket, error)
	// Detail returns the ticket with its follows.
	Detail(ctx context.Context, id int64) (*ticket.Details, error)
	// Reply appends a staff reply written by from, moves the ticket to
	// Waiting and returns the status it had before.
	Reply(ctx context.Context, id int64, from, content string, inTopic bool) (previous uint8, err error)
	SetStatus(ctx context.Context, id int64, status uint8, inTopic bool) error
}

// Subscriptions is the bot's port onto the subscription domain.
type Subscriptions interface {
	Find(ctx context.Context, id int64) (*usersub.Subscribe, error)
	// ListByUser returns the user's subscriptions with their plans, active
	// ones first.
	ListByUser(ctx context.Context, userID int64) ([]*usersub.SubscribeDetails, error)
	// ResetTraffic zeroes the subscription's used traffic.
	ResetTraffic(ctx context.Context, sub *usersub.Subscribe) error
	// SetStatus moves the subscription to status.
	SetStatus(ctx context.Context, sub *usersub.Subscribe, status uint8) error
}

// Billing is the bot's port onto the billing domain; amounts are in cents.
type Billing interface {
	// Revenue returns the order total of the day of t.
	Revenue(ctx context.Context, t time.Time) (int64, error)
	// Balance returns the user's wallet balance.
	Balance(ctx context.Context, userID int64) (int64, error)
}

// AuditLogs is the bot's port onto the platform's audit log.
type AuditLogs interface {
	// RecentLogins returns up to limit of the user's login log entries,
	// newest first.
	RecentLogins(ctx context.Context, userID int64, limit int) ([]*log.SystemLog, error)
	// Insert records a log row: the administrators' mutations made through
	// the bot.
	Insert(ctx context.Context, row *log.SystemLog) error
}
