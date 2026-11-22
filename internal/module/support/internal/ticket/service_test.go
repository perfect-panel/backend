package ticket

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The ticket service runs over the harness repositories with the Telegram
// mirror recorded.

var _ Notifier = (*supporttest.Notifier)(nil)

func newDesk(t *testing.T, limiter CreationLimiter) (*supporttest.Env, *Service, *supporttest.Notifier) {
	t.Helper()
	env, svc, mirror, _ := newAuditedDesk(t, Limits{Creation: limiter})
	return env, svc, mirror
}

// newAuditedDesk builds the desk with limits and an audit trail recorded.
func newAuditedDesk(t *testing.T, limits Limits) (*supporttest.Env, *Service, *supporttest.Notifier, *supporttest.AuditLog) {
	t.Helper()
	logtest.Discard(t)
	env := supporttest.New(t)
	mirror := &supporttest.Notifier{}
	audit := &supporttest.AuditLog{}
	return env, NewService(env.Tickets, mirror, limits, audit), mirror, audit
}

func statusPtr(status uint8) *uint8 { return &status }

// Whoever writes last decides who a ticket waits for, from whatever status
// it was in: a reply from staff (the admin panel or the bot) hands it to the
// user, a reply from the user hands it back to staff. A closed ticket is the
// exception for staff: it takes no staff reply until it is explicitly
// reopened, while the owner's reply reopens it. Staff may move a ticket to
// any status, the user only close it. Every change is mirrored as what it
// was.
func TestTicketStatusTransitions(t *testing.T) {
	const owner = 11
	actions := []struct {
		name   string
		run    func(s *Service, id int64) error
		want   uint8
		thread []string // the follow the action stores, "from:content"
		mirror string   // "reply" or "status"
		// staffReply marks a reply staff write, which a closed ticket refuses.
		staffReply bool
	}{
		{"staff reply", func(s *Service, id int64) error {
			return s.CreateFollow(context.Background(), &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "try again"})
		}, entity.Waiting, []string{"System:try again"}, "reply", true},
		{"bot reply", func(s *Service, id int64) error {
			_, err := s.UpdateAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Reply: "fixed", From: "admin"})
			return err
		}, entity.Waiting, []string{"admin:fixed"}, "reply", true},
		{"user reply", func(s *Service, id int64) error {
			return s.CreateUserFollow(supporttest.WithUser(context.Background(), owner), &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "still broken"})
		}, entity.Pending, []string{"User:still broken"}, "reply", false},
		{"staff marks processed", func(s *Service, id int64) error {
			return s.UpdateStatus(context.Background(), &dto.UpdateTicketStatusRequest{Id: id, Status: statusPtr(entity.Processed)})
		}, entity.Processed, nil, "status", false},
		{"bot closes", func(s *Service, id int64) error {
			_, err := s.UpdateAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Status: entity.Closed})
			return err
		}, entity.Closed, nil, "status", false},
		{"user closes", func(s *Service, id int64) error {
			return s.UpdateUserStatus(supporttest.WithUser(context.Background(), owner), &dto.UpdateUserTicketStatusRequest{Id: id, Status: statusPtr(entity.Closed)})
		}, entity.Closed, nil, "status", false},
	}
	for _, from := range []uint8{entity.Pending, entity.Waiting, entity.Processed, entity.Closed} {
		for _, action := range actions {
			t.Run(fmt.Sprintf("%s from %d", action.name, from), func(t *testing.T) {
				env, svc, mirror := newDesk(t, nil)
				id := env.Ticket(t, entity.Ticket{UserId: owner, Status: from}).Id

				err := action.run(svc, id)
				if from == entity.Closed && action.staffReply {
					expectClosedRefusal(t, env, mirror, id, err)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if got := env.ReloadTicket(t, id).Status; got != action.want {
					t.Fatalf("status = %d, want %d", got, action.want)
				}
				var thread []string
				for _, f := range env.Follows(t, id) {
					thread = append(thread, f.From+":"+f.Content)
				}
				if !reflect.DeepEqual(thread, action.thread) {
					t.Fatalf("thread = %v, want %v", thread, action.thread)
				}
				mirrored := map[string]int{"reply": len(mirror.Replies), "status": len(mirror.Statuses)}
				if mirrored[action.mirror] != 1 || len(mirror.Replies)+len(mirror.Statuses) != 1 {
					t.Fatalf("mirrored replies %+v, statuses %+v, want one %s", mirror.Replies, mirror.Statuses, action.mirror)
				}
			})
		}
	}
}

// expectClosedRefusal checks that a staff reply to the closed ticket id was
// refused as closed and changed or mirrored nothing.
func expectClosedRefusal(t *testing.T, env *supporttest.Env, mirror *supporttest.Notifier, id int64, err error) {
	t.Helper()
	if !errors.Is(err, entity.ErrClosed) || xerr.CodeOf(err) != xerr.InvalidParams {
		t.Fatalf("staff reply to a closed ticket: %v, want it refused as closed", err)
	}
	if env.ReloadTicket(t, id).Status != entity.Closed || len(env.Follows(t, id)) != 0 || len(mirror.Replies)+len(mirror.Statuses) != 0 {
		t.Fatal("a refused staff reply changed or mirrored the closed ticket")
	}
}

// A user's status change is scoped to their own ticket even when the
// ownership check has passed: the update itself matches the owner.
func TestUserStatusChangeIsOwnerScoped(t *testing.T) {
	env, svc, _ := newDesk(t, nil)
	mine := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Waiting}).Id
	theirs := env.Ticket(t, entity.Ticket{UserId: 12, Status: entity.Waiting}).Id

	if err := svc.UpdateUserStatus(supporttest.WithUser(context.Background(), 11), &dto.UpdateUserTicketStatusRequest{Id: theirs, Status: statusPtr(entity.Closed)}); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("closing another's ticket: %v, want invalid access", err)
	}
	if err := svc.UpdateUserStatus(supporttest.WithUser(context.Background(), 11), &dto.UpdateUserTicketStatusRequest{Id: mine, Status: statusPtr(entity.Closed)}); err != nil {
		t.Fatal(err)
	}
	if env.ReloadTicket(t, mine).Status != entity.Closed || env.ReloadTicket(t, theirs).Status != entity.Waiting {
		t.Fatal("the close reached the wrong ticket")
	}
}

// seedDesk stores the tickets the list tests page through, ids 1 to 5.
func seedDesk(t *testing.T, env *supporttest.Env) {
	t.Helper()
	for _, row := range []entity.Ticket{
		{Title: "cannot connect", Description: "since the update", UserId: 1, Status: entity.Pending},
		{Title: "refund", Description: "charged twice", UserId: 2, Status: entity.Closed},
		{Title: "slow speed", Description: "evenings only", UserId: 1, Status: entity.Waiting},
		{Title: "invoice", Description: "company name", UserId: 2, Status: entity.Processed},
		{Title: "cannot pay", Description: "card declined", UserId: 1, Status: entity.Closed},
	} {
		env.Ticket(t, row)
	}
}

func ids(list []dto.Ticket) []int64 {
	out := []int64{}
	for _, item := range list {
		out = append(out, item.Id)
	}
	return out
}

// The desk list hides closed tickets unless a status is asked for, narrows
// to one user and to a search of title and description, and pages newest
// first; the total counts every match.
func TestDeskListFiltersAndPages(t *testing.T) {
	env, svc, _ := newDesk(t, nil)
	seedDesk(t, env)
	for _, tc := range []struct {
		name  string
		req   dto.GetTicketListRequest
		total int64
		ids   []int64
	}{
		{"open tickets", dto.GetTicketListRequest{Page: 1, Size: 10}, 3, []int64{4, 3, 1}},
		{"closed tickets", dto.GetTicketListRequest{Page: 1, Size: 10, Status: statusPtr(entity.Closed)}, 2, []int64{5, 2}},
		{"one user", dto.GetTicketListRequest{Page: 1, Size: 10, UserId: 1}, 2, []int64{3, 1}},
		{"one user's closed", dto.GetTicketListRequest{Page: 1, Size: 10, UserId: 1, Status: statusPtr(entity.Closed)}, 1, []int64{5}},
		{"search in titles", dto.GetTicketListRequest{Page: 1, Size: 10, Search: "cannot"}, 1, []int64{1}},
		{"search in descriptions", dto.GetTicketListRequest{Page: 1, Size: 10, Search: "evening", Status: statusPtr(entity.Waiting)}, 1, []int64{3}},
		{"second page", dto.GetTicketListRequest{Page: 2, Size: 2}, 3, []int64{1}},
		{"past the end", dto.GetTicketListRequest{Page: 3, Size: 2}, 3, []int64{}},
		// Out-of-range paging is normalized to the first page of the
		// default size.
		{"no paging", dto.GetTicketListRequest{}, 3, []int64{4, 3, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.List(context.Background(), &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Total != tc.total || !reflect.DeepEqual(ids(resp.List), tc.ids) {
				t.Fatalf("list = %d %v, want %d %v", resp.Total, ids(resp.List), tc.total, tc.ids)
			}
		})
	}
}

// A user's list is always theirs, filtered and paged like the desk's.
func TestUserListShowsOnlyTheirTickets(t *testing.T) {
	env, svc, _ := newDesk(t, nil)
	seedDesk(t, env)
	ctx := supporttest.WithUser(context.Background(), 1)
	for _, tc := range []struct {
		name  string
		req   dto.GetUserTicketListRequest
		total int64
		ids   []int64
	}{
		{"open tickets", dto.GetUserTicketListRequest{Page: 1, Size: 10}, 2, []int64{3, 1}},
		{"closed tickets", dto.GetUserTicketListRequest{Page: 1, Size: 10, Status: statusPtr(entity.Closed)}, 1, []int64{5}},
		{"searched", dto.GetUserTicketListRequest{Page: 1, Size: 10, Search: "update"}, 1, []int64{1}},
		{"another user's search", dto.GetUserTicketListRequest{Page: 1, Size: 10, Search: "invoice"}, 0, []int64{}},
		{"second page", dto.GetUserTicketListRequest{Page: 2, Size: 1}, 2, []int64{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.GetUserList(ctx, &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Total != tc.total || !reflect.DeepEqual(ids(resp.List), tc.ids) {
				t.Fatalf("list = %d %v, want %d %v", resp.Total, ids(resp.List), tc.total, tc.ids)
			}
		})
	}
	if _, err := svc.GetUserList(context.Background(), &dto.GetUserTicketListRequest{Page: 1, Size: 10}); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("anonymous list: %v, want invalid access", err)
	}
}

// A thread reads in the order it was written, whatever order the follows
// were stored in.
func TestTicketThreadIsChronological(t *testing.T) {
	env, svc, _ := newDesk(t, nil)
	id := env.Ticket(t, entity.Ticket{UserId: 11}).Id
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	env.Follow(t, entity.Follow{TicketId: id, From: "System", Content: "second", CreatedAt: start.Add(2 * time.Minute)})
	env.Follow(t, entity.Follow{TicketId: id, From: entity.FromUser, Content: "first", CreatedAt: start.Add(time.Minute)})
	env.Follow(t, entity.Follow{TicketId: id, From: entity.FromUser, Content: "third", CreatedAt: start.Add(2 * time.Minute)})

	for name, read := range map[string]func() (*dto.Ticket, error){
		"desk": func() (*dto.Ticket, error) { return svc.GetDetail(context.Background(), &dto.GetTicketRequest{Id: id}) },
		"user": func() (*dto.Ticket, error) {
			return svc.GetUserDetail(supporttest.WithUser(context.Background(), 11), &dto.GetUserTicketDetailRequest{Id: id})
		},
	} {
		detail, err := read()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var thread []string
		for _, f := range detail.Follows {
			thread = append(thread, f.Content)
		}
		if !reflect.DeepEqual(thread, []string{"first", "second", "third"}) {
			t.Fatalf("%s thread = %v, want first, second, third", name, thread)
		}
	}
}

// fixedLimiter grants or refuses every ticket, or fails.
type fixedLimiter struct {
	allow bool
	err   error
	asked []int64
}

func (l *fixedLimiter) Allow(_ context.Context, userID int64) (bool, error) {
	l.asked = append(l.asked, userID)
	return l.allow, l.err
}

// The creation limit refuses a flood without storing or mirroring the
// ticket, and fails open when it cannot be checked.
func TestNewTicketsFollowTheCreationLimit(t *testing.T) {
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
			env, svc, mirror := newDesk(t, tc.limiter)
			err := svc.CreateUserTicket(supporttest.WithUser(context.Background(), 11), &dto.CreateUserTicketRequest{Title: "help", Description: "now"})
			if stored := err == nil; stored != tc.stored {
				t.Fatalf("CreateUserTicket: %v, want stored %v", err, tc.stored)
			}
			if !tc.stored && xerr.CodeOf(err) != xerr.TooManyRequests {
				t.Fatalf("refusal = %v, want too many requests", err)
			}
			n := env.Count(t, &entity.Ticket{})
			if (n == 1) != tc.stored || len(mirror.Created) != int(n) || !reflect.DeepEqual(tc.limiter.asked, []int64{11}) {
				t.Fatalf("%d tickets, mirrored %v, limiter asked %v", n, mirror.Created, tc.limiter.asked)
			}
		})
	}
}

// Five tickets an hour per user, counted in Redis; without Redis there is
// no limit.
func TestCreationLimiterAllowsFivePerHour(t *testing.T) {
	if NewCreationLimiter(nil) != nil {
		t.Fatal("a limiter without Redis limits")
	}
	env := supporttest.New(t)
	limiter := NewCreationLimiter(env.Redis)
	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		allowed, err := limiter.Allow(ctx, 11)
		if err != nil || allowed != (i <= 5) {
			t.Fatalf("ticket %d: allowed %v (err %v)", i, allowed, err)
		}
	}
	if allowed, err := limiter.Allow(ctx, 12); err != nil || !allowed {
		t.Fatalf("another user: allowed %v (err %v)", allowed, err)
	}
	env.Mini.FastForward(time.Hour)
	if allowed, err := limiter.Allow(ctx, 11); err != nil || !allowed {
		t.Fatalf("after the window: allowed %v (err %v)", allowed, err)
	}
	env.Mini.Close()
	if _, err := limiter.Allow(ctx, 11); err == nil {
		t.Fatal("the limit answered without Redis")
	}
}

// A write the store refuses is reported under the failing operation's code
// and is not mirrored.
func TestTicketWritesReportTheStoreFailure(t *testing.T) {
	refused := errors.New("disk full")
	userCtx := supporttest.WithUser(context.Background(), 11)
	for _, tc := range []struct {
		name            string
		op, table       string
		run             func(s *Service, id int64) error
		code            uint32
		followsExpected int64
	}{
		{"new ticket", "create", "ticket", func(s *Service, _ int64) error {
			return s.CreateUserTicket(userCtx, &dto.CreateUserTicketRequest{Title: "help"})
		}, xerr.DatabaseInsertError, 0},
		{"staff reply, ticket unreadable", "query", "ticket", func(s *Service, id int64) error {
			return s.CreateFollow(context.Background(), &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "hi"})
		}, xerr.DatabaseQueryError, 0},
		{"staff reply, follow refused", "create", "ticket_follow", func(s *Service, id int64) error {
			return s.CreateFollow(context.Background(), &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "hi"})
		}, xerr.DatabaseInsertError, 0},
		// The follow is written before the status: without a transaction
		// the reply stays, the status change does not.
		{"user reply, status refused", "update", "ticket", func(s *Service, id int64) error {
			return s.CreateUserFollow(userCtx, &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "hi"})
		}, xerr.DatabaseUpdateError, 1},
		{"status refused", "update", "ticket", func(s *Service, id int64) error {
			return s.UpdateStatus(context.Background(), &dto.UpdateTicketStatusRequest{Id: id, Status: statusPtr(entity.Closed)})
		}, xerr.DatabaseUpdateError, 0},
		{"bot status, ticket unreadable", "query", "ticket", func(s *Service, id int64) error {
			_, err := s.UpdateAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Status: entity.Closed})
			return err
		}, xerr.DatabaseQueryError, 0},
		{"bot status refused", "update", "ticket", func(s *Service, id int64) error {
			_, err := s.UpdateAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Status: entity.Closed})
			return err
		}, xerr.DatabaseUpdateError, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, svc, mirror := newDesk(t, nil)
			id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Waiting}).Id
			lift := env.Refuse(t, tc.op, tc.table, 0, refused)

			err := tc.run(svc, id)
			lift()
			if xerr.CodeOf(err) != tc.code || !errors.Is(err, refused) {
				t.Fatalf("error = %v, want code %d wrapping the store's refusal", err, tc.code)
			}
			if got := env.ReloadTicket(t, id).Status; got != entity.Waiting {
				t.Fatalf("status = %d, want it unchanged", got)
			}
			if n := env.Count(t, &entity.Follow{}); n != tc.followsExpected {
				t.Fatalf("%d follows, want %d", n, tc.followsExpected)
			}
			if len(mirror.Created)+len(mirror.Replies)+len(mirror.Statuses) != 0 {
				t.Fatalf("mirrored %+v, want nothing", mirror)
			}
		})
	}
}

// A read the store refuses is reported as a query failure.
func TestTicketReadsReportTheStoreFailure(t *testing.T) {
	refused := errors.New("connection reset")
	userCtx := supporttest.WithUser(context.Background(), 11)
	for name, read := range map[string]func(s *Service) error{
		"desk list": func(s *Service) error {
			_, err := s.List(context.Background(), &dto.GetTicketListRequest{Page: 1, Size: 10})
			return err
		},
		"desk detail": func(s *Service) error {
			_, err := s.GetDetail(context.Background(), &dto.GetTicketRequest{Id: 1})
			return err
		},
		"user list": func(s *Service) error {
			_, err := s.GetUserList(userCtx, &dto.GetUserTicketListRequest{Page: 1, Size: 10})
			return err
		},
		"user detail": func(s *Service) error {
			_, err := s.GetUserDetail(userCtx, &dto.GetUserTicketDetailRequest{Id: 1})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			env, svc, _ := newDesk(t, nil)
			env.Ticket(t, entity.Ticket{UserId: 11})
			env.Refuse(t, "query", "ticket", 0, refused)
			if err := read(svc); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, refused) {
				t.Fatalf("error = %v, want a query failure wrapping the store's refusal", err)
			}
		})
	}
}

// Only the four ticket statuses exist: neither the bot nor the admin panel
// can move a ticket to any other, and the ticket stays as it was.
func TestStaffCannotSetAnUnknownStatus(t *testing.T) {
	for _, status := range []uint8{0, 5, 9, 255} {
		for name, update := range map[string]func(*Service, int64) error{
			"bot": func(s *Service, id int64) error {
				_, err := s.UpdateAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Status: status})
				return err
			},
			"admin panel": func(s *Service, id int64) error {
				return s.UpdateStatus(context.Background(), &dto.UpdateTicketStatusRequest{Id: id, Status: statusPtr(status)})
			},
		} {
			env, svc, mirror := newDesk(t, nil)
			id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Waiting}).Id
			err := update(svc, id)
			if xerr.CodeOf(err) != xerr.InvalidParams || env.ReloadTicket(t, id).Status != entity.Waiting || len(mirror.Statuses) != 0 {
				t.Fatalf("%s, status %d: error = %v, want it refused and nothing changed", name, status, err)
			}
		}
	}
}

// A status change for a ticket that does not exist is reported, and nothing
// is mirrored for it.
func TestStaffStatusChangeNeedsTheTicket(t *testing.T) {
	_, svc, mirror := newDesk(t, nil)
	err := svc.UpdateStatus(context.Background(), &dto.UpdateTicketStatusRequest{Id: 404, Status: statusPtr(entity.Closed)})
	if xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) || len(mirror.Statuses) != 0 {
		t.Fatalf("error = %v, mirrored = %v; want not found and nothing mirrored", err, mirror.Statuses)
	}
}

// Without a mirror channel wired, replies and status changes still apply.
func TestDeskWorksWithoutMirror(t *testing.T) {
	logtest.Discard(t)
	env := supporttest.New(t)
	svc := NewService(env.Tickets, nil, Limits{}, nil)
	id := env.Ticket(t, entity.Ticket{UserId: 11}).Id
	if err := svc.CreateFollow(context.Background(), &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: entity.FollowText, Content: "on it"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateUserStatus(supporttest.WithUser(context.Background(), 11), &dto.UpdateUserTicketStatusRequest{Id: id, Status: statusPtr(entity.Closed)}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateUserTicket(supporttest.WithUser(context.Background(), 11), &dto.CreateUserTicketRequest{Title: "again"}); err != nil {
		t.Fatal(err)
	}
	if env.ReloadTicket(t, id).Status != entity.Closed || len(env.Follows(t, id)) != 1 || env.Count(t, &entity.Ticket{}) != 2 {
		t.Fatal("the desk did not apply the changes")
	}
}

// An image reply is stored as an image with the reference the client sent;
// a reference the clients would not render as a raster image is refused.
func TestUserImageRepliesAreStoredAsSent(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		stored        bool
	}{
		{"inline webp", "data:image/webp;base64,UklGRg==", true},
		{"https image", "https://cdn.example.com/screenshot.png", true},
		{"inline svg", "data:image/svg+xml;base64,PHN2Zy8+", false},
		{"relative path", "/uploads/screenshot.png", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, svc, _ := newDesk(t, nil)
			id := env.Ticket(t, entity.Ticket{UserId: 11}).Id
			err := svc.CreateUserFollow(supporttest.WithUser(context.Background(), 11), &dto.CreateUserTicketFollowRequest{TicketId: id, Type: entity.FollowImage, Content: tc.content})
			follows := env.Follows(t, id)
			if !tc.stored {
				if xerr.CodeOf(err) != xerr.InvalidParams || len(follows) != 0 {
					t.Fatalf("error = %v, follows = %+v, want the reply refused", err, follows)
				}
				return
			}
			if err != nil || len(follows) != 1 || follows[0].Type != entity.FollowImage || follows[0].Content != tc.content {
				t.Fatalf("error = %v, follows = %+v, want the image stored as sent", err, follows)
			}
		})
	}
}

// A user request without a signed-in user, or about a ticket that does not
// exist, is refused and changes nothing.
func TestUserRequestsNeedAUserAndATicket(t *testing.T) {
	anonymous, owner := context.Background(), supporttest.WithUser(context.Background(), 11)
	for _, tc := range []struct {
		name string
		run  func(s *Service, id int64) error
		code uint32
	}{
		{"anonymous new ticket", func(s *Service, _ int64) error {
			return s.CreateUserTicket(anonymous, &dto.CreateUserTicketRequest{Title: "help"})
		}, xerr.InvalidAccess},
		{"anonymous reply", func(s *Service, id int64) error {
			return s.CreateUserFollow(anonymous, &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "hi"})
		}, xerr.InvalidAccess},
		{"anonymous detail", func(s *Service, id int64) error {
			_, err := s.GetUserDetail(anonymous, &dto.GetUserTicketDetailRequest{Id: id})
			return err
		}, xerr.InvalidAccess},
		{"anonymous close", func(s *Service, id int64) error {
			return s.UpdateUserStatus(anonymous, &dto.UpdateUserTicketStatusRequest{Id: id, Status: statusPtr(entity.Closed)})
		}, xerr.InvalidAccess},
		{"another user's detail", func(s *Service, id int64) error {
			_, err := s.GetUserDetail(supporttest.WithUser(context.Background(), 12), &dto.GetUserTicketDetailRequest{Id: id})
			return err
		}, xerr.InvalidAccess},
		{"reply to a missing ticket", func(s *Service, id int64) error {
			return s.CreateUserFollow(owner, &dto.CreateUserTicketFollowRequest{TicketId: id + 1, Content: "hi"})
		}, xerr.DatabaseQueryError},
		{"detail of a missing ticket", func(s *Service, id int64) error {
			_, err := s.GetUserDetail(owner, &dto.GetUserTicketDetailRequest{Id: id + 1})
			return err
		}, xerr.DatabaseQueryError},
		{"close a missing ticket", func(s *Service, id int64) error {
			return s.UpdateUserStatus(owner, &dto.UpdateUserTicketStatusRequest{Id: id + 1, Status: statusPtr(entity.Closed)})
		}, xerr.DatabaseQueryError},
		{"reopen", func(s *Service, id int64) error {
			return s.UpdateUserStatus(owner, &dto.UpdateUserTicketStatusRequest{Id: id, Status: statusPtr(entity.Pending)})
		}, xerr.InvalidParams},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, svc, mirror := newDesk(t, nil)
			id := env.Ticket(t, entity.Ticket{UserId: 11, Status: entity.Waiting}).Id
			if err := tc.run(svc, id); xerr.CodeOf(err) != tc.code {
				t.Fatalf("error = %v, want code %d", err, tc.code)
			}
			if env.Count(t, &entity.Ticket{}) != 1 || env.ReloadTicket(t, id).Status != entity.Waiting || env.Count(t, &entity.Follow{}) != 0 {
				t.Fatal("a refused request changed the desk")
			}
			if len(mirror.Created)+len(mirror.Replies)+len(mirror.Statuses) != 0 {
				t.Fatalf("mirrored %+v, want nothing", mirror)
			}
		})
	}
}
