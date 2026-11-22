package ticket

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	ticketEntity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The user ticket handlers run against the real support facade over the
// harness database, with the ticket creation limit on miniredis and the
// Telegram mirror recorded. Each case starts from three tickets: 1 (user 11,
// pending, with the user's first message), 2 (user 12, closed) and 3 (user
// 11, waiting for the user).

const (
	owner    = 11
	stranger = 12
)

type userWorld struct {
	env    *supporttest.Env
	h      *server.Hertz
	mirror *supporttest.Notifier
}

// userFixture serves the routes to the signed-in user userID, or to an
// anonymous caller when userID is 0; the auth middleware puts the user into
// the request context.
func userFixture(t *testing.T, userID int64) userWorld {
	t.Helper()
	w := userWorld{env: supporttest.New(t), mirror: &supporttest.Notifier{}}
	w.env.Ticket(t, ticketEntity.Ticket{Title: "cannot connect", Description: "since today", UserId: owner, Status: ticketEntity.Pending})
	w.env.Follow(t, ticketEntity.Follow{TicketId: 1, From: ticketEntity.FromUser, Content: "the iOS client times out"})
	w.env.Ticket(t, ticketEntity.Ticket{Title: "refund", UserId: stranger, Status: ticketEntity.Closed})
	w.env.Ticket(t, ticketEntity.Ticket{Title: "slow speed", Description: "evenings", UserId: owner, Status: ticketEntity.Waiting})
	svc := support.New(support.Deps{Tickets: w.env.Tickets, TicketNotify: w.mirror, Redis: w.env.Redis})
	w.h = server.New()
	if userID != 0 {
		w.h.Use(func(ctx context.Context, c *app.RequestContext) { c.Next(supporttest.WithUser(ctx, userID)) })
	}
	group := w.h.Group("/v1/public/ticket")
	group.PUT("/", UpdateUserTicketStatusHandler(svc))
	group.POST("/", CreateUserTicketHandler(svc))
	group.GET("/detail", GetUserTicketDetailsHandler(svc))
	group.POST("/follow", CreateUserTicketFollowHandler(svc))
	group.GET("/list", GetUserTicketListHandler(svc))
	return w
}

// unchanged fails the test unless the tickets and threads are as the
// fixture stored them and nothing was mirrored.
func (w userWorld) unchanged(t *testing.T) {
	t.Helper()
	statuses := []uint8{w.env.ReloadTicket(t, 1).Status, w.env.ReloadTicket(t, 2).Status, w.env.ReloadTicket(t, 3).Status}
	if !reflect.DeepEqual(statuses, []uint8{ticketEntity.Pending, ticketEntity.Closed, ticketEntity.Waiting}) ||
		w.env.Count(t, &ticketEntity.Ticket{}) != 3 || w.env.Count(t, &ticketEntity.Follow{}) != 1 {
		t.Fatalf("ticket statuses = %v, want the tickets unchanged", statuses)
	}
	if len(w.mirror.Created)+len(w.mirror.Replies)+len(w.mirror.Statuses) != 0 {
		t.Fatalf("mirrored %+v, want nothing", w.mirror)
	}
}

func TestUserTicketHandlersServeTheirOwnTickets(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		user                 int64
		method, target, body string
		check                func(t *testing.T, w userWorld, reply supporttest.Reply)
	}{
		{"open", owner, http.MethodPost, "/v1/public/ticket/", `{"title":"billing","description":"charged twice"}`,
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := w.env.ReloadTicket(t, 4)
				if got.Title != "billing" || got.Description != "charged twice" || got.UserId != owner || got.Status != ticketEntity.Pending {
					t.Fatalf("ticket = %+v, want a pending ticket of the user", got)
				}
				if !reflect.DeepEqual(w.mirror.Created, []int64{4}) {
					t.Fatalf("mirrored = %v, want the new ticket", w.mirror.Created)
				}
			}},
		// Whatever author the client claims, the reply is the user's; the
		// ticket waits for staff again.
		{"reply", owner, http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3,"from":"System","type":1,"content":"still slow"}`,
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				follows := w.env.Follows(t, 3)
				if got := w.env.ReloadTicket(t, 3); got.Status != ticketEntity.Pending || len(follows) != 1 || follows[0].From != ticketEntity.FromUser {
					t.Fatalf("ticket = %+v, follows = %+v, want the user's reply and the ticket pending", got, follows)
				}
				if want := []supporttest.MirroredReply{{TicketID: 3, From: ticketEntity.FromUser, Content: "still slow"}}; !reflect.DeepEqual(w.mirror.Replies, want) {
					t.Fatalf("mirrored = %+v, want %+v", w.mirror.Replies, want)
				}
			}},
		{"reply with a screenshot", owner, http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3,"type":2,"content":"data:image/png;base64,iVBORw0KGgo="}`,
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				if follows := w.env.Follows(t, 3); len(follows) != 1 || follows[0].Type != ticketEntity.FollowImage {
					t.Fatalf("follows = %+v, want the image reply", follows)
				}
			}},
		// The clients render an image reply as <img src>: anything but a
		// raster image or an http(s) URL is refused.
		{"reply with a script as image", owner, http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3,"type":2,"content":"javascript:alert(1)"}`,
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.InvalidParams, "Param Error")
				w.unchanged(t)
			}},
		{"detail", owner, http.MethodGet, "/v1/public/ticket/detail?id=1", "",
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, supporttest.TicketView(w.env.ReloadTicket(t, 1), w.env.Follows(t, 1)...))
			}},
		{"detail missing", owner, http.MethodGet, "/v1/public/ticket/detail?id=9", "",
			func(t *testing.T, _ userWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
			}},
		// The user sees their open tickets, newest first; closed ones only
		// on request.
		{"list", owner, http.MethodGet, "/v1/public/ticket/list?page=1&size=10", "",
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetUserTicketListResponse{Total: 2, List: []dto.Ticket{
					supporttest.TicketView(w.env.ReloadTicket(t, 3)), supporttest.TicketView(w.env.ReloadTicket(t, 1)),
				}})
			}},
		{"list closed", owner, http.MethodGet, "/v1/public/ticket/list?page=1&size=10&status=4", "",
			func(t *testing.T, _ userWorld, reply supporttest.Reply) {
				reply.OK(t, `{"total":0,"list":[]}`)
			}},
		{"list searched", owner, http.MethodGet, "/v1/public/ticket/list?page=1&size=10&search=today", "",
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetUserTicketListResponse{Total: 1, List: []dto.Ticket{supporttest.TicketView(w.env.ReloadTicket(t, 1))}})
			}},
		{"close", owner, http.MethodPut, "/v1/public/ticket/", `{"id":3,"status":4}`,
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				if got := w.env.ReloadTicket(t, 3); got.Status != ticketEntity.Closed {
					t.Fatalf("ticket = %+v, want it closed", got)
				}
				if want := []supporttest.MirroredStatus{{TicketID: 3, Status: ticketEntity.Closed}}; !reflect.DeepEqual(w.mirror.Statuses, want) {
					t.Fatalf("mirrored = %+v, want %+v", w.mirror.Statuses, want)
				}
			}},
		// Closing is the only status change users have.
		{"reopen", owner, http.MethodPut, "/v1/public/ticket/", `{"id":3,"status":1}`,
			func(t *testing.T, w userWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.InvalidParams, "Param Error")
				w.unchanged(t)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := userFixture(t, tc.user)
			tc.check(t, w, supporttest.Serve(t, w.h, tc.method, tc.target, tc.body))
		})
	}
}

// Another user's ticket is neither readable nor writable, and an anonymous
// caller has no tickets at all: both answer invalid access and change
// nothing.
func TestUserTicketHandlersRefuseOthersTickets(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		user                 int64
		method, target, body string
	}{
		{"reply to another's ticket", owner, http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":2,"content":"me too"}`},
		{"detail of another's ticket", owner, http.MethodGet, "/v1/public/ticket/detail?id=2", ""},
		{"close another's ticket", stranger, http.MethodPut, "/v1/public/ticket/", `{"id":1,"status":4}`},
		{"anonymous open", 0, http.MethodPost, "/v1/public/ticket/", `{"title":"help"}`},
		{"anonymous reply", 0, http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":1,"content":"hi"}`},
		{"anonymous detail", 0, http.MethodGet, "/v1/public/ticket/detail?id=1", ""},
		{"anonymous list", 0, http.MethodGet, "/v1/public/ticket/list?page=1&size=10", ""},
		{"anonymous close", 0, http.MethodPut, "/v1/public/ticket/", `{"id":1,"status":4}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := userFixture(t, tc.user)
			supporttest.Serve(t, w.h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidAccess, "Invalid access")
			w.unchanged(t)
		})
	}
}

// Every ticket opens a topic in the Telegram admin group, so a user may open
// five an hour; the sixth is refused as too many requests.
func TestUserTicketHandlersLimitNewTickets(t *testing.T) {
	w := userFixture(t, owner)
	for i := 0; i < 5; i++ {
		supporttest.Serve(t, w.h, http.MethodPost, "/v1/public/ticket/", `{"title":"help"}`).OK(t, nil)
	}
	supporttest.Serve(t, w.h, http.MethodPost, "/v1/public/ticket/", `{"title":"help"}`).Refused(t, xerr.TooManyRequests, "Too Many Requests")
	if n := w.env.Count(t, &ticketEntity.Ticket{}); n != 8 || len(w.mirror.Created) != 5 {
		t.Fatalf("%d tickets, %d mirrored, want the sixth neither stored nor mirrored", n, len(w.mirror.Created))
	}
}

// A request that does not bind or misses a required field is refused as a
// parameter error and changes nothing.
func TestUserTicketHandlersRefuseMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		msg                        string
	}{
		{"open not JSON", http.MethodPost, "/v1/public/ticket/", `{"title":"help"`, ""},
		{"open without title", http.MethodPost, "/v1/public/ticket/", `{"description":"no subject"}`, "Title is a required field"},
		{"open title too long", http.MethodPost, "/v1/public/ticket/", `{"title":"` + strings.Repeat("t", 256) + `"}`, "Title must be a maximum of 255 characters"},
		{"open description too long", http.MethodPost, "/v1/public/ticket/", `{"title":"help","description":"` + strings.Repeat("d", 10001) + `"}`, "Description must be a maximum of 10,000 characters"},
		{"reply not JSON", http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3,"content":}`, ""},
		{"reply ticket not a number", http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":"3","content":"hi"}`, ""},
		{"reply without ticket", http.MethodPost, "/v1/public/ticket/follow", `{"content":"hi"}`, "TicketId is a required field"},
		{"reply without content", http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3}`, "Content is a required field"},
		{"reply too long", http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3,"content":"` + strings.Repeat("c", 65536) + `"}`, "Content must be a maximum of 65,535 characters"},
		{"reply of an unknown type", http.MethodPost, "/v1/public/ticket/follow", `{"ticket_id":3,"type":7,"content":"hi"}`, "Type must be one of"},
		{"detail id not a number", http.MethodGet, "/v1/public/ticket/detail?id=latest", "", "bind Id"},
		{"detail without id", http.MethodGet, "/v1/public/ticket/detail", "", "Id is a required field"},
		{"list page not a number", http.MethodGet, "/v1/public/ticket/list?page=next&size=10", "", "bind Page"},
		{"list without page", http.MethodGet, "/v1/public/ticket/list?size=10", "", "Page is a required field"},
		{"close not JSON", http.MethodPut, "/v1/public/ticket/", `{"id":3,`, ""},
		{"close without status", http.MethodPut, "/v1/public/ticket/", `{"id":3}`, "Status is a required field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := userFixture(t, owner)
			supporttest.Serve(t, w.h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidParams, tc.msg)
			w.unchanged(t)
		})
	}
}
