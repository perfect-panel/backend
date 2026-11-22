package ticket

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The reply limit refuses a flood of replies without storing or mirroring
// them, and fails open when it cannot be checked; the creation limit is not
// the one asked.
func TestUserRepliesFollowTheReplyLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limiter *fixedLimiter
		stored  bool
	}{
		{"granted", &fixedLimiter{allow: true}, true},
		{"refused", &fixedLimiter{}, false},
		{"limit unavailable", &fixedLimiter{err: errors.New("redis down")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creation := &fixedLimiter{}
			env, svc, mirror, _ := newAuditedDesk(t, Limits{Creation: creation, Follows: tc.limiter})
			id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Waiting}).Id
			err := svc.CreateUserFollow(supporttest.WithUser(context.Background(), 11), &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "hi"})
			if stored := err == nil; stored != tc.stored {
				t.Fatalf("CreateUserFollow: %v, want stored %v", err, tc.stored)
			}
			if !tc.stored && xerr.CodeOf(err) != xerr.TooManyRequests {
				t.Fatalf("refusal = %v, want too many requests", err)
			}
			n := env.Count(t, &entity.Follow{})
			if (n == 1) != tc.stored || len(mirror.Replies) != int(n) || !reflect.DeepEqual(tc.limiter.asked, []int64{11}) || len(creation.asked) != 0 {
				t.Fatalf("%d follows, mirrored %v, reply limiter asked %v, creation limiter asked %v", n, mirror.Replies, tc.limiter.asked, creation.asked)
			}
		})
	}
}

// Thirty replies an hour per user, counted in Redis apart from the tickets
// opened; without Redis there is no limit.
func TestFollowLimiterAllowsThirtyPerHour(t *testing.T) {
	if NewFollowLimiter(nil) != nil {
		t.Fatal("a limiter without Redis limits")
	}
	env := supporttest.New(t)
	follows, creation := NewFollowLimiter(env.Redis), NewCreationLimiter(env.Redis)
	ctx := context.Background()
	for i := 1; i <= 31; i++ {
		allowed, err := follows.Allow(ctx, 11)
		if err != nil || allowed != (i <= 30) {
			t.Fatalf("reply %d: allowed %v (err %v)", i, allowed, err)
		}
	}
	if allowed, err := creation.Allow(ctx, 11); err != nil || !allowed {
		t.Fatalf("a ticket after the replies: allowed %v (err %v), want the limits counted apart", allowed, err)
	}
	env.Mini.FastForward(time.Hour)
	if allowed, err := follows.Allow(ctx, 11); err != nil || !allowed {
		t.Fatalf("after the window: allowed %v (err %v)", allowed, err)
	}
}

// Closing ends the conversation for staff: a closed ticket takes no staff
// reply, from the admin panel or the bot, until it is reopened explicitly;
// once reopened it takes replies again. The owner's reply is the exception
// and reopens the ticket itself.
func TestClosedTicketTakesNoStaffReplyUntilReopened(t *testing.T) {
	env, svc, mirror := newDesk(t, nil)
	id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Closed}).Id
	ctx := context.Background()

	err := svc.CreateFollow(ctx, &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "hello?"})
	if !errors.Is(err, entity.ErrClosed) {
		t.Fatalf("admin panel reply to a closed ticket: %v, want it refused as closed", err)
	}
	if _, err := svc.UpdateAsStaff(ctx, &dto.StaffTicketUpdateCommand{TicketId: id, Reply: "hello?", From: "admin"}); !errors.Is(err, entity.ErrClosed) {
		t.Fatalf("bot reply to a closed ticket: %v, want it refused as closed", err)
	}
	if len(env.Follows(t, id)) != 0 || len(mirror.Replies) != 0 {
		t.Fatal("a refused reply was stored or mirrored")
	}

	// The explicit reopen.
	if _, err := svc.UpdateAsStaff(ctx, &dto.StaffTicketUpdateCommand{TicketId: id, Status: entity.Pending}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateFollow(ctx, &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "hello again"}); err != nil {
		t.Fatalf("reply after the reopen: %v", err)
	}
	if got := env.ReloadTicket(t, id).Status; got != entity.Waiting || len(env.Follows(t, id)) != 1 {
		t.Fatalf("status = %d, follows = %d; want the reopened ticket waiting for the user with the reply stored", got, len(env.Follows(t, id)))
	}

	// The owner reopens a closed ticket by replying.
	closed := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Closed}).Id
	if err := svc.CreateUserFollow(supporttest.WithUser(ctx, 11), &dto.CreateUserTicketFollowRequest{TicketId: closed, Content: "not fixed"}); err != nil {
		t.Fatalf("owner's reply to a closed ticket: %v", err)
	}
	if got := env.ReloadTicket(t, closed).Status; got != entity.Pending {
		t.Fatalf("status = %d, want the owner's reply to reopen the ticket", got)
	}
}

// The administrators' replies and status changes leave an audit trail with
// the acting administrator and the ticket; the user's writes and the bot's
// (which records its own) do not.
func TestAdminTicketActionsAreAudited(t *testing.T) {
	env, svc, _, audit := newAuditedDesk(t, Limits{})
	id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Pending}).Id
	ctx := supporttest.Context()

	if err := svc.CreateFollow(ctx, &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "on it"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateStatus(ctx, &dto.UpdateTicketStatusRequest{Id: id, Status: statusPtr(entity.Closed)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAsStaff(ctx, &dto.StaffTicketUpdateCommand{TicketId: id, Status: entity.Pending}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateUserFollow(supporttest.WithUser(ctx, 11), &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "thanks"}); err != nil {
		t.Fatal(err)
	}

	actions := audit.Actions(t)
	if len(actions) != 2 {
		t.Fatalf("audited %d actions, want the reply and the status change: %+v", len(actions), actions)
	}
	for i, want := range []struct{ action, detail string }{{"ticket.reply", ""}, {"ticket.status", "status=4"}} {
		got := actions[i]
		if got.Action != want.action || got.Detail != want.detail || got.Object != "ticket" || got.ObjectID != id ||
			got.ActorID != supporttest.ActorID || got.ClientIP != supporttest.ClientIP || got.Source != log.AdminActionSourceHTTP || got.Timestamp == 0 {
			t.Fatalf("action %d = %+v, want %s on ticket %d by administrator %d", i, got, want.action, id, supporttest.ActorID)
		}
		if row := audit.Rows[i]; row.ObjectID != supporttest.ActorID || row.Type != log.TypeAdminAction.Uint8() {
			t.Fatalf("row %d = %+v, want it filed under the administrator", i, row)
		}
	}
}

// A trail that cannot be written is logged: the ticket change stands.
func TestAuditFailureDoesNotFailTheTicketChange(t *testing.T) {
	env, svc, _, audit := newAuditedDesk(t, Limits{})
	audit.Err = errors.New("log table locked")
	id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Pending}).Id
	if err := svc.CreateFollow(supporttest.Context(), &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "on it"}); err != nil {
		t.Fatalf("CreateFollow: %v", err)
	}
	if env.ReloadTicket(t, id).Status != entity.Waiting {
		t.Fatal("the reply was not applied")
	}
}
