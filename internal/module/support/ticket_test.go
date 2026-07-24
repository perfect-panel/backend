package support_test

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	ticketEntity "github.com/perfect-panel/server/internal/model/entity/ticket"
	"github.com/perfect-panel/server/internal/module/support"
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
	follows       []*ticketEntity.Follow
	statusUpdates []statusUpdate
}

func (f *fakeTicketRepo) Insert(_ context.Context, data *ticketEntity.Ticket) error {
	f.inserted = data
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
