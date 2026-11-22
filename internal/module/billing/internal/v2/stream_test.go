package v2

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/auth/token"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/infra/rediskeys"
	dto "github.com/perfect-panel/server/internal/module/billing/contract"
	"github.com/perfect-panel/server/internal/module/billing/entity/order"
	"github.com/perfect-panel/server/internal/module/billing/internal/orderevents"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// streamOrder is a user's pending order and the ticket that streams it.
type streamOrder struct {
	orderNo string
	ticket  string
	ctx     context.Context
}

func (f *v2Fixture) streamOrder() streamOrder {
	f.t.Helper()
	_, ctx := f.buyer(0, 0)
	resp, err := f.checkout.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: f.plan(10).Id, Quantity: 1, Payment: f.epay().Id})
	if err != nil {
		f.t.Fatalf("Purchase: %v", err)
	}
	orderInfo := f.h.ReloadOrder(resp.OrderNo)
	ticket, _, err := f.svc.mintEventTicket(ctx, orderInfo, "")
	if err != nil {
		f.t.Fatalf("mint ticket: %v", err)
	}
	return streamOrder{orderNo: resp.OrderNo, ticket: ticket, ctx: ctx}
}

func (f *v2Fixture) pay(orderNo string) {
	f.t.Helper()
	if _, err := f.h.Store.Order().MarkOrderPaid(context.Background(), orderNo, "trade-"+orderNo); err != nil {
		f.t.Fatal(err)
	}
}

func (f *v2Fixture) fulfil(orderNo string) {
	f.t.Helper()
	if _, err := f.h.Store.Order().UpdateOrderStatusFrom(context.Background(), orderNo, order.StatusPaid, order.StatusFinished); err != nil {
		f.t.Fatal(err)
	}
}

// eventID is the stored id of the index-th event of orderNo.
func (f *v2Fixture) eventID(orderNo string, index int) int64 {
	f.t.Helper()
	return f.h.Events(orderNo)[index].ID
}

func names(events []sinkEvent) []string {
	result := make([]string, len(events))
	for i, event := range events {
		result[i] = event.Name
	}
	return result
}

// Design: a client reconnecting with Last-Event-ID receives the events it
// missed, payment_paid, fulfilled and closed included, and nothing twice.
func TestStreamReplaysTheEventsAfterLastEventID(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	paid := f.streamOrder()
	f.pay(paid.orderNo)
	f.fulfil(paid.orderNo)
	created := f.eventID(paid.orderNo, 0)

	sink := newCollectingSink()
	f.openStream(paid.ctx, StreamRequest{OrderNo: paid.orderNo, Ticket: paid.ticket, AfterID: created}, sink)
	events := sink.waitFor(t, 3)
	if got := names(events); !slices.Equal(got, []string{"order.snapshot", "order.payment_paid", "order.fulfilled"}) {
		t.Fatalf("replayed %v", got)
	}
	if events[1].ID != strconv.FormatInt(f.eventID(paid.orderNo, 1), 10) || events[2].ID != strconv.FormatInt(f.eventID(paid.orderNo, 2), 10) {
		t.Fatalf("event ids = %q, %q", events[1].ID, events[2].ID)
	}

	// Reconnecting after the last event replays nothing.
	last := f.eventID(paid.orderNo, 2)
	upToDate := newCollectingSink()
	stream := f.openStream(paid.ctx, StreamRequest{OrderNo: paid.orderNo, Ticket: paid.ticket, AfterID: last}, upToDate)
	upToDate.waitFor(t, 1)
	time.Sleep(50 * time.Millisecond) // several catch-up ticks
	_ = stream.stop()
	if got := upToDate.names(); !slices.Equal(got, []string{"order.snapshot"}) {
		t.Fatalf("up-to-date reconnect delivered %v", got)
	}

	closed := f.streamOrder()
	if err := f.checkout.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: closed.orderNo}); err != nil {
		t.Fatal(err)
	}
	closedSink := newCollectingSink()
	f.openStream(closed.ctx, StreamRequest{OrderNo: closed.orderNo, Ticket: closed.ticket, AfterID: f.eventID(closed.orderNo, 0)}, closedSink)
	if got := names(closedSink.waitFor(t, 2)); !slices.Equal(got, []string{"order.snapshot", "order.closed"}) {
		t.Fatalf("closed order replayed %v", got)
	}
}

// A live stream delivers the order's events as they are written.
func TestStreamDeliversLiveEvents(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	o := f.streamOrder()
	sink := newCollectingSink()
	f.openStream(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: o.ticket}, sink)
	if got := names(sink.waitFor(t, 2)); !slices.Equal(got, []string{"order.snapshot", "order.created"}) {
		t.Fatalf("stream started with %v", got)
	}
	f.pay(o.orderNo)
	if got := names(sink.waitFor(t, 3)); got[2] != "order.payment_paid" {
		t.Fatalf("stream delivered %v", got)
	}
}

// A cursor older than the retained events cannot be replayed exactly; the
// client is told to rebuild its state from the snapshot.
func TestStreamResetsACursorOlderThanTheRetainedEvents(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	o := f.streamOrder()
	f.pay(o.orderNo)
	f.fulfil(o.orderNo)
	cursor := f.eventID(o.orderNo, 0)
	// Retention removed the created and payment events.
	if err := f.h.DB.Where("order_no = ? AND id < ?", o.orderNo, f.eventID(o.orderNo, 2)).Delete(&order.Event{}).Error; err != nil {
		t.Fatal(err)
	}
	sink := newCollectingSink()
	f.openStream(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: o.ticket, AfterID: cursor}, sink)
	if got := names(sink.waitFor(t, 3)); !slices.Equal(got, []string{"order.snapshot", "order.reset", "order.fulfilled"}) {
		t.Fatalf("stream delivered %v", got)
	}
}

// Design: SSE access control for the owner, another user, a guest and an
// expired ticket.
func TestStreamAccessControl(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	owned := f.streamOrder()
	other := f.streamOrder()

	refused := func(name string, req StreamRequest) {
		t.Helper()
		sink := newCollectingSink()
		err := f.svc.StreamEvents(context.Background(), req, sink)
		if xerr.CodeOf(err) != xerr.InvalidAccess || len(sink.snapshot()) != 0 {
			t.Fatalf("%s: StreamEvents = %v with %v, want InvalidAccess before any event", name, err, sink.names())
		}
	}
	refused("ticket of another order", StreamRequest{OrderNo: other.orderNo, Ticket: owned.ticket})
	forged, _ := token.NewJwtToken("another-secret", time.Now().Unix(), 600,
		token.WithOption("OrderNo", owned.orderNo), token.WithOption("Scope", v2EventScope))
	refused("forged ticket", StreamRequest{OrderNo: owned.orderNo, Ticket: forged})
	expired, _ := token.NewJwtToken("v2-secret", time.Now().Add(-time.Hour).Unix(), 60,
		token.WithOption("OrderNo", owned.orderNo), token.WithOption("Scope", v2EventScope))
	refused("expired ticket", StreamRequest{OrderNo: owned.orderNo, Ticket: expired})
	refused("garbage", StreamRequest{OrderNo: owned.orderNo, Ticket: "not-a-ticket"})

	// Another user can neither read the order nor mint its ticket.
	_, err := f.svc.EventTicket(other.ctx, owned.orderNo, "")
	assertCode(t, err, xerr.InvalidAccess)
	_, err = f.svc.GetOrder(other.ctx, owned.orderNo, "")
	assertCode(t, err, xerr.InvalidAccess)

	sink := newCollectingSink()
	f.openStream(context.Background(), StreamRequest{OrderNo: owned.orderNo, Ticket: owned.ticket}, sink)
	sink.waitFor(t, 1)
}

// A guest streams its order with the ticket its checkout capability mints.
func TestGuestStreamsWithItsCapability(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	plan, method := f.plan(5), f.epay()
	resp, err := f.svc.CreateAndCheckout(context.Background(), guestPurchase(plan, method), "guest-key-000001")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	if resp.CheckoutToken == "" {
		t.Fatal("the guest got no checkout capability")
	}
	_, err = f.svc.EventTicket(context.Background(), resp.Order.OrderNo, "wrong-capability")
	assertCode(t, err, xerr.InvalidAccess)
	refreshed, err := f.svc.GetOrder(context.Background(), resp.Order.OrderNo, resp.CheckoutToken)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	orderInfo := f.h.ReloadOrder(resp.Order.OrderNo)
	ticket, _, err := f.svc.mintEventTicket(context.Background(), orderInfo, resp.CheckoutToken)
	if err != nil {
		t.Fatal(err)
	}
	sink := newCollectingSink()
	f.openStream(context.Background(), StreamRequest{OrderNo: orderInfo.OrderNo, Ticket: ticket}, sink)
	events := sink.waitFor(t, 1)
	if events[0].Name != "order.snapshot" || refreshed.Order.OrderNo != orderInfo.OrderNo {
		t.Fatalf("guest stream started with %v", names(events))
	}
}

// fakeBroker hands out subscriptions a test can wake and drop, and counts
// the ones still open.
type fakeBroker struct {
	mu    sync.Mutex
	fails int
	subs  []*fakeSubscription
}

type fakeSubscription struct {
	broker *fakeBroker
	wake   chan struct{}
	closed bool
}

func (b *fakeBroker) Subscribe(context.Context, string) (Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fails > 0 {
		b.fails--
		return nil, errors.New("broker unavailable")
	}
	sub := &fakeSubscription{broker: b, wake: make(chan struct{}, 1)}
	b.subs = append(b.subs, sub)
	return sub, nil
}

func (s *fakeSubscription) Wake() <-chan struct{} { return s.wake }

func (s *fakeSubscription) Close() error {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	s.closed = true
	return nil
}

// counts reports how many subscriptions were taken and how many are open.
func (b *fakeBroker) counts() (taken, open int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		if !sub.closed {
			open++
		}
	}
	return len(b.subs), open
}

func (b *fakeBroker) latest() *fakeSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subs[len(b.subs)-1]
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A stream holds at most one subscription, retakes one after a failure or
// a dropped broker connection, wakes on publication and releases every
// subscription it took when it ends. A subscription taken on a retry used
// to stay open after its stream ended.
func TestStreamSubscriptionLifecycle(t *testing.T) {
	broker := &fakeBroker{fails: 1}
	f := newV2Fixture(t, v2Options{stream: func(d *StreamDeps) {
		d.Broker = broker
		// Only a wake-up delivers events promptly.
		d.Timing = StreamTiming{Heartbeat: time.Hour, CatchUp: time.Hour, Resubscribe: 10 * time.Millisecond}
	}})
	o := f.streamOrder()
	sink := newCollectingSink()
	stream := f.openStream(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: o.ticket}, sink)
	sink.waitFor(t, 2)

	waitUntil(t, "the failed subscription is retaken", func() bool { taken, _ := broker.counts(); return taken == 1 })
	time.Sleep(50 * time.Millisecond) // several resubscribe ticks
	if taken, open := broker.counts(); taken != 1 || open != 1 {
		t.Fatalf("subscriptions taken=%d open=%d, want one held", taken, open)
	}

	f.pay(o.orderNo)
	broker.latest().wake <- struct{}{}
	if got := names(sink.waitFor(t, 3)); got[2] != "order.payment_paid" {
		t.Fatalf("woken stream delivered %v", got)
	}

	close(broker.latest().wake) // the broker dropped the subscription
	waitUntil(t, "a dropped subscription is replaced", func() bool { taken, open := broker.counts(); return taken == 2 && open == 1 })

	if err := stream.stop(); err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	if _, open := broker.counts(); open != 0 {
		t.Fatalf("%d subscriptions left open after the stream ended", open)
	}
}

// One ticket holds a bounded number of concurrent streams; a refused stream
// receives nothing.
func TestStreamRefusesTooManyStreamsPerTicket(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	o := f.streamOrder()
	var streams []*runningStream
	for range maxStreamsPerTicket {
		sink := newCollectingSink()
		streams = append(streams, f.openStream(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: o.ticket}, sink))
		sink.waitFor(t, 1)
	}
	refused := newCollectingSink()
	if err := f.svc.StreamEvents(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: o.ticket}, refused); !errors.Is(err, ErrTooManyStreams) || len(refused.snapshot()) != 0 {
		t.Fatalf("StreamEvents = %v with %v, want ErrTooManyStreams before any event", err, refused.names())
	}
	_ = streams[0].stop()
	admitted := newCollectingSink()
	f.openStream(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: o.ticket}, admitted)
	admitted.waitFor(t, 1)
}

// A stream ends with a notice when its ticket expires; the client fetches a
// fresh ticket to continue.
func TestStreamEndsWhenItsTicketExpires(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	o := f.streamOrder()
	orderInfo := f.h.ReloadOrder(o.orderNo)
	ticket, err := token.NewJwtToken("v2-secret", time.Now().Unix(), 2,
		token.WithOption("OrderNo", o.orderNo), token.WithOption("Scope", v2EventScope), token.WithOption("UserId", orderInfo.UserId))
	if err != nil {
		t.Fatal(err)
	}
	sink := newCollectingSink()
	stream := f.openStream(o.ctx, StreamRequest{OrderNo: o.orderNo, Ticket: ticket}, sink)
	if err := stream.wait(t); err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	got := sink.names()
	if len(got) == 0 || got[len(got)-1] != "stream.expiring" {
		t.Fatalf("stream delivered %v, want it to end with stream.expiring", got)
	}
}

// The subscriber has a dedicated Redis connection while Publish uses the
// command hook. Both must resolve the same physical channel.
func TestOrderStreamUsesProjectChannel(t *testing.T) {
	server := miniredis.RunT(t)
	cfg := config.RedisConfig{Host: server.Addr(), KeyPrefix: "shop"}
	client := cfg.NewClient()
	t.Cleanup(func() { _ = client.Close() })
	broker := RedisBroker{Client: client, Prefix: rediskeys.New(cfg.KeyPrefix)}
	ctx := t.Context()
	sub, err := broker.Subscribe(ctx, "order-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	b := orderevents.RedisBroadcaster{Client: client}
	if err := b.Broadcast(ctx, &order.Event{OrderNo: "order-1", ID: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-sub.Wake():
		if !ok {
			t.Fatal("subscription closed before the broadcast")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("project-scoped order stream missed the broadcast")
	}
}

func TestRedisWakeupsRetainOneReplayWhileStreamIsBusy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		messages := make(chan *redis.Message)
		wake := redisWakeups(messages)
		messages <- &redis.Message{}
		messages <- &redis.Message{}
		close(messages)
		synctest.Wait() // the forwarder has finished before the stream begins reading
		if _, ok := <-wake; !ok {
			t.Fatal("no replay pending after a burst")
		}
		if _, ok := <-wake; ok {
			t.Fatal("burst scheduled a second replay")
		}
	})
}
