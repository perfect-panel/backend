package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
)

// The web client and the support module write user follows as "User"; only
// other authors are staff. Older rows spell it in lowercase or leave it empty.
var followAuthors = map[string]bool{"User": true, "user": true, "": true, "System": false, "admin": false}

func TestTicketRepliedLabelsAuthorsCorrectly(t *testing.T) {
	for from, isUser := range followAuthors {
		repo := &fakeTopicRepo{}
		if err := repo.Insert(context.Background(), &telegramtopic.Topic{
			ChatId: testGroupID, Kind: telegramtopic.KindTicket, RefId: 321, ThreadId: 12, Status: telegramtopic.StatusActive,
		}); err != nil {
			t.Fatal(err)
		}
		messenger := &recordingMessenger{}
		svc := NewTopicService(&fakeTopicClient{}, repo, testGroupID)

		if err := svc.TicketReplied(context.Background(), messenger, 321, from, "hi"); err != nil {
			t.Fatalf("from %q: TicketReplied: %v", from, err)
		}
		want := "💻 网站回复（管理员）"
		if isUser {
			want = "👤 用户回复"
		}
		if len(messenger.sent) != 1 || !strings.HasPrefix(messenger.sent[0].message, want) {
			t.Fatalf("from %q: sent = %+v, want the %q label", from, messenger.sent, want)
		}
	}
}

func TestTicketDetailLabelsAuthorsCorrectly(t *testing.T) {
	for from, isUser := range followAuthors {
		h := newAdminHarness()
		h.tickets.details = &ticket.Details{
			Id: 321, Title: "help", UserId: 7, Status: ticket.Pending,
			Follows: []ticket.Follow{{From: from, Content: "hi"}},
		}

		got := h.run("/tk 321")
		want := "📝 客服"
		if isUser {
			want = "📝 用户"
		}
		if !strings.Contains(got, want) {
			t.Fatalf("from %q: detail = %q, want the %q label", from, got, want)
		}
	}
}

func TestTicketListPaginates(t *testing.T) {
	h := newAdminHarness()
	for id := int64(1); id <= 12; id++ {
		h.tickets.tickets[id] = &ticket.Ticket{Id: id, Title: "t", Status: ticket.Pending}
	}
	got := h.run("/tickets")
	if !strings.Contains(got, "(第1/2页，共12单)") || !strings.Contains(got, "下一页：/tickets_2") {
		t.Fatalf("list = %q, want page 1 of 2", got)
	}
	if got := newAdminHarness().run("/tickets_waiting"); got != "暂无工单。" {
		t.Fatalf("empty list = %q", got)
	}
}

// The ticket list offers "/tk_<id>" and "/rp_<id>" shortcuts; tapping one
// must run the command it names instead of being ignored.
func TestTicketShortcutsRunTheirCommand(t *testing.T) {
	h := newAdminHarness()
	h.tickets.details = &ticket.Details{Id: 321, Title: "help", UserId: 7, Status: ticket.Pending}
	if got := h.run("/tk_321"); !strings.Contains(got, "321") {
		t.Fatalf("/tk_321 replied %q, want the ticket detail", got)
	}

	h = newAdminHarness()
	h.tickets.tickets = map[int64]*ticket.Ticket{321: {Id: 321, Status: ticket.Pending}}
	h.run("/rp_321 on it")
	if len(h.tickets.replies) != 1 || h.tickets.replies[0].id != 321 || h.tickets.replies[0].content != "on it" {
		t.Fatalf("/rp_321 replies = %+v, want one reply to ticket 321", h.tickets.replies)
	}
}

func TestExpandShortcut(t *testing.T) {
	for _, tc := range []struct{ command, args, wantCommand, wantArgs string }{
		{"tk_12", "", "tk", "12"},
		{"rp_12", "text here", "rp", "12 text here"},
		{"close_7", "", "close", "7"},
		{"reopen_7", "", "reopen", "7"},
		{"tickets_2", "", "tickets", "2"},
		{"reset_5", "", "reset", "5"},
		{"toggle_5", "", "toggle", "5"},
		{"ban_9", "", "ban", "9"},
		{"user_9", "", "user", "9"},
		{"user_sub_9", "", "user_sub", "9"},
		{"user_log_9", "", "user_log", "9"},
		{"tk_x", "", "tk_x", ""},
		{"tk_", "", "tk_", ""},
		{"user_sub", "5", "user_sub", "5"},
		{"user_log", "", "user_log", ""},
		{"tickets_waiting", "", "tickets_waiting", ""},
		{"confirm_ab12", "", "confirm_ab12", ""},
	} {
		gotCommand, gotArgs := expandShortcut(tc.command, tc.args)
		if gotCommand != tc.wantCommand || gotArgs != tc.wantArgs {
			t.Errorf("expandShortcut(%q, %q) = %q, %q; want %q, %q", tc.command, tc.args, gotCommand, gotArgs, tc.wantCommand, tc.wantArgs)
		}
	}
	for _, shortcut := range []string{"tk_12", "tickets_2", "user_sub_9", "ban_9"} {
		if !isAdminCommand(shortcut) {
			t.Errorf("the shortcut %q is not treated as an admin command", shortcut)
		}
	}
}
