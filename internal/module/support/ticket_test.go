package support_test

import (
	"context"
	"testing"
	"time"

	"errors"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	ticketEntity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

type statusUpdate struct {
	ticketID int64
	userID   int64
	status   uint8
}

type fakeTicketRepo struct {
	ticket        *ticketEntity.Ticket
	details       *ticketEntity.Details
	inserted      *ticketEntity.Ticket
	inserts       int
	follows       []*ticketEntity.Follow
	statusUpdates []statusUpdate
}

func (f *fakeTicketRepo) Insert(_ context.Context, data *ticketEntity.Ticket) error {
	f.inserted = data
	f.inserts++
	return nil
}

func (f *fakeTicketRepo) FindOne(_ context.Context, _ int64) (*ticketEntity.Ticket, error) {
	return f.ticket, nil
}

func (f *fakeTicketRepo) Update(_ context.Context, _ *ticketEntity.Ticket) error { return nil }

func (f *fakeTicketRepo) Delete(_ context.Context, _ int64) error { return nil }

func (f *fakeTicketRepo) QueryTicketDetail(_ context.Context, _ int64) (*ticketEntity.Details, error) {
	return f.details, nil
}

func (f *fakeTicketRepo) InsertTicketFollow(_ context.Context, data *ticketEntity.Follow) error {
	f.follows = append(f.follows, data)
	return nil
}

func (f *fakeTicketRepo) QueryTicketList(_ context.Context, _, _ int, _ int64, _ *uint8, _ string) (int64, []*ticketEntity.Ticket, error) {
	return 0, nil, nil
}

func (f *fakeTicketRepo) UpdateTicketStatus(_ context.Context, id, userID int64, status uint8) error {
	f.statusUpdates = append(f.statusUpdates, statusUpdate{ticketID: id, userID: userID, status: status})
	return nil
}

func (f *fakeTicketRepo) QueryWaitReplyTotal(_ context.Context) (int64, error) { return 0, nil }

func newTicketService(repo *fakeTicketRepo) support.Service {
	return support.New(support.Deps{Tickets: repo})
}

func TestCreateUserTicketUsesContextUser(t *testing.T) {
	repo := &fakeTicketRepo{}
	svc := newTicketService(repo)

	err := svc.CreateUserTicket(ctxWithUser(11), &dto.CreateUserTicketRequest{Title: "help"})
	if err != nil {
		t.Fatalf("CreateUserTicket: %v", err)
	}
	if repo.inserted == nil || repo.inserted.UserId != 11 || repo.inserted.Status != ticketEntity.Pending {
		t.Fatalf("unexpected inserted ticket: %+v", repo.inserted)
	}
}

func TestCreateUserTicketRejectsAnonymous(t *testing.T) {
	repo := &fakeTicketRepo{}
	svc := newTicketService(repo)

	if err := svc.CreateUserTicket(context.Background(), &dto.CreateUserTicketRequest{Title: "x"}); err == nil {
		t.Fatal("anonymous request must be rejected")
	}
	if repo.inserted != nil {
		t.Fatal("ticket must not be inserted for anonymous user")
	}
}

func TestCreateUserTicketFollowEnforcesOwnership(t *testing.T) {
	repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 99}}
	svc := newTicketService(repo)

	err := svc.CreateUserTicketFollow(ctxWithUser(11), &dto.CreateUserTicketFollowRequest{TicketId: 1})
	if err == nil {
		t.Fatal("follow on someone else's ticket must be rejected")
	}
	if len(repo.follows) != 0 {
		t.Fatal("no follow may be inserted on ownership violation")
	}
}

func TestCreateUserTicketFollowFlipsStatusToPending(t *testing.T) {
	repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 11}}
	svc := newTicketService(repo)

	err := svc.CreateUserTicketFollow(ctxWithUser(11), &dto.CreateUserTicketFollowRequest{TicketId: 1, Content: "hi"})
	if err != nil {
		t.Fatalf("CreateUserTicketFollow: %v", err)
	}
	if len(repo.follows) != 1 {
		t.Fatalf("follow not inserted: %+v", repo.follows)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != (statusUpdate{ticketID: 1, userID: 11, status: ticketEntity.Pending}) {
		t.Fatalf("user reply must flip status to Pending scoped to the user: %+v", repo.statusUpdates)
	}
}

func TestCreateTicketFollowFlipsStatusToWaiting(t *testing.T) {
	repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 99}}
	svc := newTicketService(repo)

	err := svc.CreateTicketFollow(context.Background(), &dto.CreateTicketFollowRequest{TicketId: 1, Content: "re"})
	if err != nil {
		t.Fatalf("CreateTicketFollow: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != (statusUpdate{ticketID: 1, userID: 0, status: ticketEntity.Waiting}) {
		t.Fatalf("admin reply must flip status to Waiting without user scope: %+v", repo.statusUpdates)
	}
}

func TestGetUserTicketDetailsEnforcesOwnership(t *testing.T) {
	repo := &fakeTicketRepo{details: &ticketEntity.Details{Id: 1, UserId: 99}}
	svc := newTicketService(repo)

	if _, err := svc.GetUserTicketDetails(ctxWithUser(11), &dto.GetUserTicketDetailRequest{Id: 1}); err == nil {
		t.Fatal("reading someone else's ticket must be rejected")
	}

	repo.details.UserId = 11
	got, err := svc.GetUserTicketDetails(ctxWithUser(11), &dto.GetUserTicketDetailRequest{Id: 1})
	if err != nil {
		t.Fatalf("GetUserTicketDetails: %v", err)
	}
	if got.Id != 1 {
		t.Fatalf("unexpected detail: %+v", got)
	}
}

type ticketReply struct {
	ticketID      int64
	from, content string
}

type ticketStatusMirror struct {
	ticketID int64
	status   uint8
}

type fakeTicketNotifier struct {
	created  []int64
	replies  []ticketReply
	statuses []ticketStatusMirror
}

func (n *fakeTicketNotifier) TicketCreated(_ context.Context, t *ticketEntity.Ticket) {
	n.created = append(n.created, t.Id)
}

func (n *fakeTicketNotifier) TicketReplied(_ context.Context, ticketID int64, from, content string) {
	n.replies = append(n.replies, ticketReply{ticketID: ticketID, from: from, content: content})
}

func (n *fakeTicketNotifier) TicketStatusChanged(_ context.Context, ticketID int64, status uint8) {
	n.statuses = append(n.statuses, ticketStatusMirror{ticketID: ticketID, status: status})
}

func errCode(t *testing.T, err error) uint32 {
	t.Helper()
	var codeErr *xerr.CodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("error %v carries no error code", err)
	}
	return codeErr.GetErrCode()
}

// A user may only close their own ticket. The ownership check must come
// before the status mirror: the owner-scoped UPDATE silently matches nothing
// for someone else's ticket, but the mirror would still close or reopen that
// ticket's topic in the Telegram admin group.
func TestUpdateUserTicketStatusRejectsForeignTicket(t *testing.T) {
	repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 99}}
	notify := &fakeTicketNotifier{}
	svc := support.New(support.Deps{Tickets: repo, TicketNotify: notify})

	err := svc.UpdateUserTicketStatus(ctxWithUser(11), &dto.UpdateUserTicketStatusRequest{Id: 1, Status: ptr(uint8(ticketEntity.Closed))})

	if err == nil || errCode(t, err) != xerr.InvalidAccess {
		t.Fatalf("error = %v, want invalid access", err)
	}
	if len(repo.statusUpdates) != 0 || len(notify.statuses) != 0 {
		t.Fatalf("updates = %+v, mirrored = %+v, want nothing for a foreign ticket", repo.statusUpdates, notify.statuses)
	}
}

func TestUpdateUserTicketStatusAllowsOnlyClosing(t *testing.T) {
	for _, status := range []*uint8{nil, ptr(uint8(ticketEntity.Pending)), ptr(uint8(ticketEntity.Waiting)), ptr(uint8(ticketEntity.Processed)), ptr(uint8(9))} {
		repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 11}}
		notify := &fakeTicketNotifier{}
		svc := support.New(support.Deps{Tickets: repo, TicketNotify: notify})

		err := svc.UpdateUserTicketStatus(ctxWithUser(11), &dto.UpdateUserTicketStatusRequest{Id: 1, Status: status})

		if err == nil || errCode(t, err) != xerr.InvalidParams {
			t.Fatalf("status %v: error = %v, want invalid params", status, err)
		}
		if len(repo.statusUpdates) != 0 || len(notify.statuses) != 0 {
			t.Fatalf("status %v: updates = %+v, mirrored = %+v, want nothing", status, repo.statusUpdates, notify.statuses)
		}
	}
}

func TestUpdateUserTicketStatusClosesOwnTicket(t *testing.T) {
	repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 11}}
	notify := &fakeTicketNotifier{}
	svc := support.New(support.Deps{Tickets: repo, TicketNotify: notify})

	if err := svc.UpdateUserTicketStatus(ctxWithUser(11), &dto.UpdateUserTicketStatusRequest{Id: 1, Status: ptr(uint8(ticketEntity.Closed))}); err != nil {
		t.Fatalf("UpdateUserTicketStatus: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != (statusUpdate{ticketID: 1, userID: 11, status: ticketEntity.Closed}) {
		t.Fatalf("updates = %+v, want the owner-scoped close", repo.statusUpdates)
	}
	if len(notify.statuses) != 1 || notify.statuses[0] != (ticketStatusMirror{ticketID: 1, status: ticketEntity.Closed}) {
		t.Fatalf("mirrored = %+v, want the close mirrored", notify.statuses)
	}
}

// Whatever the client claims, a user's follow is written as the user's: a
// forged "System"/"admin" author would render as a staff reply in the admin
// panel, in /tk and in the Telegram mirror.
func TestCreateUserTicketFollowForcesUserAuthor(t *testing.T) {
	for _, claimed := range []string{"System", "admin", "User", ""} {
		repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 11}}
		notify := &fakeTicketNotifier{}
		svc := support.New(support.Deps{Tickets: repo, TicketNotify: notify})

		err := svc.CreateUserTicketFollow(ctxWithUser(11), &dto.CreateUserTicketFollowRequest{TicketId: 1, From: claimed, Type: ticketEntity.FollowText, Content: "hi"})
		if err != nil {
			t.Fatalf("from %q: CreateUserTicketFollow: %v", claimed, err)
		}
		if len(repo.follows) != 1 || repo.follows[0].From != ticketEntity.FromUser {
			t.Fatalf("from %q: follows = %+v, want the user as author", claimed, repo.follows)
		}
		if len(notify.replies) != 1 || notify.replies[0].from != ticketEntity.FromUser {
			t.Fatalf("from %q: mirrored replies = %+v, want the user as author", claimed, notify.replies)
		}
	}
}

func TestCreateUserTicketFollowAcceptsOnlyUserFollowTypes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		typ      uint8
		content  string
		wantType uint8 // 0 means refused
	}{
		{name: "text", typ: ticketEntity.FollowText, content: "hello", wantType: ticketEntity.FollowText},
		{name: "omitted type is text", typ: 0, content: "hello", wantType: ticketEntity.FollowText},
		{name: "web client image", typ: ticketEntity.FollowImage, content: "data:image/webp;base64,UklGRg==", wantType: ticketEntity.FollowImage},
		{name: "png data url", typ: ticketEntity.FollowImage, content: "DATA:image/PNG;base64,iVBORw0KGgo=", wantType: ticketEntity.FollowImage},
		{name: "https image", typ: ticketEntity.FollowImage, content: "https://cdn.example.com/a.png", wantType: ticketEntity.FollowImage},
		{name: "http image", typ: ticketEntity.FollowImage, content: "http://cdn.example.com/a.png", wantType: ticketEntity.FollowImage},
		{name: "javascript url", typ: ticketEntity.FollowImage, content: "javascript:alert(1)"},
		{name: "svg data url", typ: ticketEntity.FollowImage, content: "data:image/svg+xml;base64,PHN2Zy8+"},
		{name: "html data url", typ: ticketEntity.FollowImage, content: "data:text/html;base64,PGgxPg=="},
		{name: "relative path", typ: ticketEntity.FollowImage, content: "/uploads/a.png"},
		{name: "empty image", typ: ticketEntity.FollowImage, content: ""},
		{name: "empty data url", typ: ticketEntity.FollowImage, content: "data:image/webp;base64,"},
		{name: "unknown type", typ: 3, content: "hello"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTicketRepo{ticket: &ticketEntity.Ticket{Id: 1, UserId: 11}}
			svc := newTicketService(repo)

			err := svc.CreateUserTicketFollow(ctxWithUser(11), &dto.CreateUserTicketFollowRequest{TicketId: 1, Type: tt.typ, Content: tt.content})

			if tt.wantType == 0 {
				if err == nil || errCode(t, err) != xerr.InvalidParams || len(repo.follows) != 0 {
					t.Fatalf("error = %v, follows = %+v, want the follow refused", err, repo.follows)
				}
				return
			}
			if err != nil || len(repo.follows) != 1 || repo.follows[0].Type != tt.wantType {
				t.Fatalf("error = %v, follows = %+v, want one follow of type %d", err, repo.follows, tt.wantType)
			}
		})
	}
}

func TestCreateUserTicketIsRateLimitedPerUser(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	repo := &fakeTicketRepo{}
	notify := &fakeTicketNotifier{}
	svc := support.New(support.Deps{Tickets: repo, TicketNotify: notify, Redis: client})

	for i := 0; i < 5; i++ {
		if err := svc.CreateUserTicket(ctxWithUser(11), &dto.CreateUserTicketRequest{Title: "help"}); err != nil {
			t.Fatalf("ticket %d within the quota: %v", i+1, err)
		}
	}
	err := svc.CreateUserTicket(ctxWithUser(11), &dto.CreateUserTicketRequest{Title: "help"})
	if err == nil || errCode(t, err) != xerr.TooManyRequests {
		t.Fatalf("sixth ticket error = %v, want too many requests", err)
	}
	if repo.inserts != 5 || len(notify.created) != 5 {
		t.Fatalf("inserts = %d, topics = %d, want the refused ticket neither stored nor mirrored", repo.inserts, len(notify.created))
	}
	if err := svc.CreateUserTicket(ctxWithUser(12), &dto.CreateUserTicketRequest{Title: "help"}); err != nil {
		t.Fatalf("another user's ticket: %v", err)
	}

	server.FastForward(time.Hour)
	if err := svc.CreateUserTicket(ctxWithUser(11), &dto.CreateUserTicketRequest{Title: "help"}); err != nil {
		t.Fatalf("ticket after the window: %v", err)
	}
}

// The limit protects staff from floods; a Redis outage must not close the
// ticket desk.
func TestCreateUserTicketFailsOpenWithoutRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	server.Close()
	repo := &fakeTicketRepo{}
	svc := support.New(support.Deps{Tickets: repo, Redis: client})

	if err := svc.CreateUserTicket(ctxWithUser(11), &dto.CreateUserTicketRequest{Title: "help"}); err != nil {
		t.Fatalf("CreateUserTicket with Redis down: %v", err)
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts = %d, want the ticket stored", repo.inserts)
	}
}
