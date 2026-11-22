package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tgbot "github.com/go-telegram/bot"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
)

func seededTopic(t *testing.T) (*fakeTopicRepo, *telegramtopic.Topic) {
	t.Helper()
	repo := &fakeTopicRepo{}
	topic := &telegramtopic.Topic{ChatId: testGroupID, Kind: telegramtopic.KindTicket, RefId: 321, ThreadId: 12, Status: telegramtopic.StatusActive}
	if err := repo.Insert(context.Background(), topic); err != nil {
		t.Fatal(err)
	}
	return repo, topic
}

// A website reply longer than a Telegram message is mirrored cut to the
// limit instead of being rejected whole; so is a long ticket description.
func TestTicketMirrorIsClampedToTelegramMessageLimit(t *testing.T) {
	repo, _ := seededTopic(t)
	messenger := &recordingMessenger{}
	svc := NewTopicService(&fakeTopicClient{}, repo, testGroupID)
	long := strings.Repeat("长", 5000)

	if err := svc.TicketReplied(context.Background(), messenger, 321, "User", long); err != nil {
		t.Fatalf("TicketReplied: %v", err)
	}
	if err := svc.TicketCreated(context.Background(), messenger, &ticket.Ticket{Id: 322, Title: "help", Description: long}, "buyer@example.com"); err != nil {
		t.Fatalf("TicketCreated: %v", err)
	}
	if len(messenger.sent) != 2 {
		t.Fatalf("sent = %d messages, want the reply and the opening message", len(messenger.sent))
	}
	for _, sent := range messenger.sent {
		if n := utf8.RuneCountInString(sent.message); n != telegramMessageLimit {
			t.Fatalf("mirrored message is %d characters, want cut to %d", n, telegramMessageLimit)
		}
		if !strings.HasSuffix(sent.message, "…") {
			t.Fatal("a cut message does not say so")
		}
	}
	if got := messenger.sent[0].message; !strings.HasPrefix(got, "👤 用户回复") {
		t.Fatalf("reply = %q, want the author label kept", got[:20])
	}
}

// throttledOp is a delivery Telegram throttles for the first rejections.
type throttledOp struct {
	rejections int
	retryAfter int
	calls      int
}

func (o *throttledOp) run(int64) error {
	o.calls++
	if o.calls <= o.rejections {
		return &tgbot.TooManyRequestsError{Message: "too many requests, Too Many Requests: retry after 1", RetryAfter: o.retryAfter}
	}
	return nil
}

// A throttled delivery is not a broken topic: Relay recreates or reopens
// nothing, waits out a short retry_after and delivers once more.
func TestRelayWaitsOutAShortRateLimit(t *testing.T) {
	repo, topic := seededTopic(t)
	client := &fakeTopicClient{deadThreads: map[int64]bool{}, closedThreads: map[int64]bool{}}
	svc := NewTopicService(client, repo, testGroupID)
	op := &throttledOp{rejections: 1, retryAfter: 1}

	start := time.Now()
	relayed, err := svc.Relay(context.Background(), topic, op.run)
	if err != nil {
		t.Fatalf("Relay: %v", err)
	}
	if op.calls != 2 || time.Since(start) < time.Second {
		t.Fatalf("calls = %d after %v, want a second delivery after the pause Telegram asked for", op.calls, time.Since(start))
	}
	if relayed.ThreadId != 12 || len(client.reopened)+len(client.deleted)+len(client.createdNames) != 0 {
		t.Fatalf("relayed = %+v, client = %+v; want the topic left alone", relayed, client)
	}
}

// A retry_after beyond what a request-bound relay can wait, a second
// throttling, or a context that ends first is reported as the rate limit,
// distinctly from every other failure, so the caller can back off.
func TestRelayReportsARateLimitItCannotWaitOut(t *testing.T) {
	for name, tc := range map[string]struct {
		op  *throttledOp
		ctx func() (context.Context, context.CancelFunc)
	}{
		"long pause":      {&throttledOp{rejections: 1, retryAfter: 30}, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }},
		"throttled twice": {&throttledOp{rejections: 2, retryAfter: 1}, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }},
		"context too short": {&throttledOp{rejections: 1, retryAfter: 2}, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 500*time.Millisecond)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			repo, topic := seededTopic(t)
			client := &fakeTopicClient{deadThreads: map[int64]bool{}, closedThreads: map[int64]bool{}}
			svc := NewTopicService(client, repo, testGroupID)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := svc.Relay(ctx, topic, tc.op.run)
			var limited *RateLimitedError
			if !errors.Is(err, ErrRateLimited) || !errors.As(err, &limited) || limited.RetryAfter != time.Duration(tc.op.retryAfter)*time.Second {
				t.Fatalf("Relay error = %v, want the rate limit with retry_after %ds", err, tc.op.retryAfter)
			}
			if !errors.As(err, new(*tgbot.TooManyRequestsError)) {
				t.Fatal("the bot library's rejection is not reachable through the error")
			}
			if len(client.reopened)+len(client.deleted)+len(client.createdNames) != 0 {
				t.Fatalf("client = %+v, want the topic left alone", client)
			}
		})
	}
}

// A staff reply to a closed ticket is refused by the support use case; the
// bot says so and points at /reopen instead of reporting a failure.
func TestAdminReplyToClosedTicketNamesTheReopen(t *testing.T) {
	h := newAdminHarness()
	h.tickets = newFakeTickets(&ticket.Ticket{Id: 321, Status: ticket.Closed})
	if got := h.run("/rp 321 还在吗"); !strings.Contains(got, "已关闭") || !strings.Contains(got, "/reopen_321") {
		t.Fatalf("reply = %q, want the closed ticket named with its reopen shortcut", got)
	}
	if len(h.tickets.replies) != 0 || h.tickets.tickets[321].Status != ticket.Closed {
		t.Fatal("the refused reply changed the ticket")
	}
}

// The same refusal inside the ticket's topic.
func TestTopicReplyToClosedTicketIsRefusedAloud(t *testing.T) {
	h := newRoutingHarness()
	h.tickets.tickets[321].Status = ticket.Closed
	h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusActive)
	h.dispatch(groupMessage(500, 12, "还在吗"))
	if len(h.tickets.replies) != 0 {
		t.Fatal("a reply to a closed ticket was stored")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.last().message, "已关闭") || h.messenger.last().threadID != 12 {
		t.Fatalf("sent = %+v, want the refusal in the topic", h.messenger.sent)
	}
}
