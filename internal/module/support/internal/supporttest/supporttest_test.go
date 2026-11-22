package supporttest_test

import (
	"context"
	"errors"
	"testing"

	taskEntity "github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/module/support"
	ticketEntity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/document"
	"github.com/perfect-panel/backend/internal/module/support/internal/marketing"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/internal/module/support/internal/ticket"
)

// The fakes stand in for the ports the facade and the subdomains declare.
var (
	_ support.SubscriptionReader     = (*supporttest.Subscriptions)(nil)
	_ document.SubscriptionReader    = (*supporttest.Subscriptions)(nil)
	_ support.EmailRecipientReader   = (*supporttest.Recipients)(nil)
	_ marketing.EmailRecipientReader = (*supporttest.Recipients)(nil)
	_ support.SubscriptionSelector   = (*supporttest.QuotaTargets)(nil)
	_ marketing.SubscriptionSelector = (*supporttest.QuotaTargets)(nil)
	_ support.MarketingQueue         = (*supporttest.Queue)(nil)
	_ marketing.Queue                = (*supporttest.Queue)(nil)
	_ support.BatchEmailStopper      = (*supporttest.Stopper)(nil)
	_ ticket.Notifier                = (*supporttest.Notifier)(nil)
)

// The harness repositories write the rows its readers return, and a second
// harness starts from an empty database.
func TestHarnessStoresTicketsAndTasks(t *testing.T) {
	env := supporttest.New(t)
	ctx := context.Background()
	row := &ticketEntity.Ticket{Title: "help", UserId: 3, Status: ticketEntity.Waiting}
	if err := env.Tickets.Insert(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := env.Tickets.InsertTicketFollow(ctx, &ticketEntity.Follow{TicketId: row.Id, From: "User", Type: ticketEntity.FollowText, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := env.ReloadTicket(t, row.Id); got.Title != "help" || got.UserId != 3 || got.Status != ticketEntity.Waiting {
		t.Fatalf("ticket = %+v", got)
	}
	if follows := env.Follows(t, row.Id); len(follows) != 1 || follows[0].Content != "hi" {
		t.Fatalf("follows = %+v", follows)
	}
	seeded := env.Task(t, taskEntity.Task{Type: taskEntity.TypeEmail, Total: 2})
	if got := env.ReloadTask(t, seeded.Id); got.Total != 2 || len(env.AllTasks(t)) != 1 {
		t.Fatalf("task = %+v, tasks = %+v", got, env.AllTasks(t))
	}

	if other := supporttest.New(t); other.Count(t, &ticketEntity.Ticket{}) != 0 || len(other.AllTasks(t)) != 0 {
		t.Fatal("a new harness shares the previous one's database")
	}
}

// A refusal fails only the chosen statements on the chosen table, after the
// ones it lets pass, and ends with the test that asked for it.
func TestRefuseFailsTheChosenStatements(t *testing.T) {
	env := supporttest.New(t)
	ctx := context.Background()
	refused := errors.New("disk full")
	t.Run("refused", func(t *testing.T) {
		env.Refuse(t, "create", "ticket", 1, refused)
		env.Ticket(t, ticketEntity.Ticket{UserId: 1})
		if err := env.Tickets.Insert(ctx, &ticketEntity.Ticket{UserId: 2}); !errors.Is(err, refused) {
			t.Fatalf("second insert: %v, want it refused", err)
		}
		env.Follow(t, ticketEntity.Follow{TicketId: 1, Content: "other tables still write"})
		if _, err := env.Tickets.FindOne(ctx, 1); err != nil {
			t.Fatalf("reads still work: %v", err)
		}
	})
	env.Ticket(t, ticketEntity.Ticket{UserId: 3})
	if n := env.Count(t, &ticketEntity.Ticket{}); n != 2 {
		t.Fatalf("%d tickets, want the refused one missing and the later one stored", n)
	}
}
