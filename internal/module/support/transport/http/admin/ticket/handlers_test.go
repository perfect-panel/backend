package ticket

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	ticketEntity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The admin ticket handlers run against the real support facade over the
// harness database, with the Telegram mirror recorded. Each case starts from
// three tickets: 1 (user 11, pending, with the user's first message), 2
// (user 12, closed) and 3 (user 11, waiting for the user).

type deskWorld struct {
	env    *supporttest.Env
	h      *server.Hertz
	mirror *supporttest.Notifier
}

func deskFixture(t *testing.T) deskWorld {
	t.Helper()
	w := deskWorld{env: supporttest.New(t), mirror: &supporttest.Notifier{}}
	w.env.Ticket(t, ticketEntity.Ticket{Title: "cannot connect", Description: "since today", UserId: 11, Status: ticketEntity.Pending})
	w.env.Follow(t, ticketEntity.Follow{TicketId: 1, From: ticketEntity.FromUser, Content: "the iOS client times out"})
	w.env.Ticket(t, ticketEntity.Ticket{Title: "refund", UserId: 12, Status: ticketEntity.Closed})
	w.env.Ticket(t, ticketEntity.Ticket{Title: "slow speed", Description: "evenings", UserId: 11, Status: ticketEntity.Waiting})
	svc := support.New(support.Deps{Tickets: w.env.Tickets, TicketNotify: w.mirror})
	w.h = server.New()
	group := w.h.Group("/v1/admin/ticket")
	group.PUT("/", UpdateTicketStatusHandler(svc))
	group.GET("/detail", GetTicketHandler(svc))
	group.POST("/follow", CreateTicketFollowHandler(svc))
	group.GET("/list", GetTicketListHandler(svc))
	return w
}

func (w deskWorld) views(t *testing.T, ids ...int64) []dto.Ticket {
	t.Helper()
	views := make([]dto.Ticket, 0, len(ids))
	for _, id := range ids {
		views = append(views, supporttest.TicketView(w.env.ReloadTicket(t, id)))
	}
	return views
}

func TestAdminTicketHandlersRunTheDesk(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		check                      func(t *testing.T, w deskWorld, reply supporttest.Reply)
	}{
		{"status", http.MethodPut, "/v1/admin/ticket/", `{"id":1,"status":3}`,
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				if got := w.env.ReloadTicket(t, 1); got.Status != ticketEntity.Processed {
					t.Fatalf("ticket = %+v, want it processed", got)
				}
				if want := []supporttest.MirroredStatus{{TicketID: 1, Status: ticketEntity.Processed}}; !reflect.DeepEqual(w.mirror.Statuses, want) {
					t.Fatalf("mirrored = %+v, want %+v", w.mirror.Statuses, want)
				}
			}},
		// A staff reply lands in the thread and hands the ticket back to
		// the user.
		{"reply", http.MethodPost, "/v1/admin/ticket/follow", `{"ticket_id":1,"from":"System","type":1,"content":"which version?"}`,
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, nil)
				follows := w.env.Follows(t, 1)
				if got := w.env.ReloadTicket(t, 1); got.Status != ticketEntity.Waiting || len(follows) != 2 ||
					follows[1].From != "System" || follows[1].Type != ticketEntity.FollowText || follows[1].Content != "which version?" {
					t.Fatalf("ticket = %+v, follows = %+v, want the reply stored and the ticket waiting", got, follows)
				}
				if want := []supporttest.MirroredReply{{TicketID: 1, From: "System", Content: "which version?"}}; !reflect.DeepEqual(w.mirror.Replies, want) {
					t.Fatalf("mirrored = %+v, want %+v", w.mirror.Replies, want)
				}
			}},
		{"reply to a missing ticket", http.MethodPost, "/v1/admin/ticket/follow", `{"ticket_id":9,"from":"System","type":1,"content":"hello?"}`,
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
				if n := w.env.Count(t, &ticketEntity.Follow{}); n != 1 || len(w.mirror.Replies) != 0 {
					t.Fatalf("%d follows, mirrored %+v, want nothing stored or mirrored", n, w.mirror.Replies)
				}
			}},
		{"detail", http.MethodGet, "/v1/admin/ticket/detail?id=1", "",
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, supporttest.TicketView(w.env.ReloadTicket(t, 1), w.env.Follows(t, 1)...))
			}},
		{"detail missing", http.MethodGet, "/v1/admin/ticket/detail?id=9", "",
			func(t *testing.T, _ deskWorld, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
			}},
		// The desk lists the open tickets, newest first, without their
		// threads.
		{"list", http.MethodGet, "/v1/admin/ticket/list?page=1&size=10", "",
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetTicketListResponse{Total: 2, List: w.views(t, 3, 1)})
			}},
		{"list closed", http.MethodGet, "/v1/admin/ticket/list?page=1&size=10&status=4", "",
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetTicketListResponse{Total: 1, List: w.views(t, 2)})
			}},
		{"list of a user", http.MethodGet, "/v1/admin/ticket/list?page=1&size=10&user_id=12&status=4", "",
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetTicketListResponse{Total: 1, List: w.views(t, 2)})
			}},
		{"list searched", http.MethodGet, "/v1/admin/ticket/list?page=1&size=10&search=evening", "",
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetTicketListResponse{Total: 1, List: w.views(t, 3)})
			}},
		{"list second page", http.MethodGet, "/v1/admin/ticket/list?page=2&size=1", "",
			func(t *testing.T, w deskWorld, reply supporttest.Reply) {
				reply.OK(t, dto.GetTicketListResponse{Total: 2, List: w.views(t, 1)})
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := deskFixture(t)
			tc.check(t, w, supporttest.Serve(t, w.h, tc.method, tc.target, tc.body))
		})
	}
}

// A request that does not bind or misses a required field is refused as a
// parameter error: no ticket changes and nothing is mirrored.
func TestAdminTicketHandlersRefuseMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		msg                        string
	}{
		{"status not JSON", http.MethodPut, "/v1/admin/ticket/", `{"id":1,"status":`, ""},
		{"status not a number", http.MethodPut, "/v1/admin/ticket/", `{"id":1,"status":"closed"}`, ""},
		{"status without status", http.MethodPut, "/v1/admin/ticket/", `{"id":1}`, "Status is a required field"},
		{"reply not JSON", http.MethodPost, "/v1/admin/ticket/follow", `{"ticket_id":1,`, ""},
		{"reply without content", http.MethodPost, "/v1/admin/ticket/follow", `{"ticket_id":1,"from":"System","type":1}`, "Content is a required field"},
		{"reply without type", http.MethodPost, "/v1/admin/ticket/follow", `{"ticket_id":1,"from":"System","content":"hi"}`, "Type is a required field"},
		{"detail id not a number", http.MethodGet, "/v1/admin/ticket/detail?id=one", "", "bind Id"},
		{"detail without id", http.MethodGet, "/v1/admin/ticket/detail", "", "Id is a required field"},
		{"list status not a number", http.MethodGet, "/v1/admin/ticket/list?page=1&size=10&status=open", "", "bind Status"},
		{"list without page", http.MethodGet, "/v1/admin/ticket/list?size=10", "", "Page is a required field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := deskFixture(t)
			supporttest.Serve(t, w.h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidParams, tc.msg)
			if got := w.env.ReloadTicket(t, 1); got.Status != ticketEntity.Pending || w.env.Count(t, &ticketEntity.Follow{}) != 1 {
				t.Fatalf("ticket = %+v, want it unchanged", got)
			}
			if len(w.mirror.Replies)+len(w.mirror.Statuses) != 0 {
				t.Fatalf("mirrored %+v %+v, want nothing", w.mirror.Replies, w.mirror.Statuses)
			}
		})
	}
}
