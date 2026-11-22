package order

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/route/param"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// streamService answers the event stream with a fixed outcome and records
// the request it served. keepAlive sends a heartbeat before the events.
type streamService struct {
	err       error
	keepAlive bool
	events    [][3]string
	got       billing.V2EventStreamRequest
}

var _ EventStreamer = (*streamService)(nil)

func (s *streamService) V2StreamOrderEvents(_ context.Context, req billing.V2EventStreamRequest, sink billing.V2EventSink) error {
	s.got = req
	if s.err != nil {
		return s.err
	}
	if s.keepAlive {
		if err := sink.KeepAlive(); err != nil {
			return err
		}
	}
	for _, event := range s.events {
		// The handler ignores what a stream that started returns.
		if err := sink.Event(event[0], event[1], []byte(event[2])); err != nil {
			return err
		}
	}
	return nil
}

// serveEvents runs the handler on a mock connection and returns the status
// and what was written: the streamed events, or a JSON refusal.
func serveEvents(t *testing.T, svc *streamService, lastEventID string) (int, string) {
	t.Helper()
	engine := server.Default()
	ctx := engine.NewContext()
	conn := mock.NewConn("")
	ctx.SetConn(conn)
	ctx.Request.Header.SetMethod(http.MethodGet)
	ctx.Request.SetRequestURI("/v2/public/orders/order-1/events?ticket=ticket-1&after=5")
	if lastEventID != "" {
		ctx.Request.Header.Set("Last-Event-ID", lastEventID)
	}
	ctx.Params = append(ctx.Params, param.Param{Key: "orderNo", Value: "order-1"})
	V2OrderEventsHandler(EventStreamDeps{Billing: svc})(context.Background(), ctx)
	if ctx.Response.GetHijackWriter() == nil {
		return ctx.Response.StatusCode(), string(ctx.Response.Body())
	}
	written := conn.WriterRecorder()
	streamed, err := written.ReadBinary(written.WroteLen())
	if err != nil {
		t.Fatal(err)
	}
	return ctx.Response.StatusCode(), string(streamed)
}

func TestV2OrderEventsHandlerStreamsTheFacadeEvents(t *testing.T) {
	svc := &streamService{events: [][3]string{{"", "order.snapshot", `{"order_no":"order-1"}`}, {"7", "order.payment_paid", `{}`}}}
	status, body := serveEvents(t, svc, "6")
	if status != http.StatusOK || !strings.Contains(body, "event: order.snapshot") || !strings.Contains(body, "id: 7") {
		t.Fatalf("response %d: %q", status, body)
	}
	// Last-Event-ID wins over the after parameter.
	if svc.got.OrderNo != "order-1" || svc.got.Ticket != "ticket-1" || svc.got.AfterID != 6 {
		t.Fatalf("stream request = %+v", svc.got)
	}
	serveEvents(t, svc, "")
	if svc.got.AfterID != 5 {
		t.Fatalf("after parameter = %d, want 5", svc.got.AfterID)
	}
}

func TestV2OrderEventsHandlerAnswersRefusalsWithJSON(t *testing.T) {
	for name, tt := range map[string]struct {
		err        error
		wantStatus int
		wantCode   uint32
	}{
		"too many streams": {billing.ErrTooManyEventStreams, http.StatusTooManyRequests, xerr.TooManyRequests},
		"invalid ticket":   {xerr.Errorf(xerr.InvalidAccess, "event ticket is invalid"), http.StatusOK, xerr.InvalidAccess},
	} {
		t.Run(name, func(t *testing.T) {
			status, body := serveEvents(t, &streamService{err: tt.err}, "")
			var response struct {
				Code uint32 `json:"code"`
			}
			if err := json.Unmarshal([]byte(body), &response); err != nil {
				t.Fatalf("body %q: %v", body, err)
			}
			if status != tt.wantStatus || response.Code != tt.wantCode {
				t.Fatalf("response %d %q, want %d with code %d", status, body, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

// A heartbeat opens the stream like an event does: a quiet order still
// reaches the browser through proxies, which must neither cache nor buffer
// the stream.
func TestV2OrderEventsHandlerOpensTheStreamOnAKeepAlive(t *testing.T) {
	status, stream := serveEvents(t, &streamService{keepAlive: true}, "")
	if status != http.StatusOK || !strings.Contains(stream, ":keep-alive\n") {
		t.Fatalf("response %d: %q, want a keep-alive comment", status, stream)
	}
	for _, header := range []string{"Content-Type: text/event-stream", "Cache-Control: no-cache", "X-Accel-Buffering: no"} {
		if !strings.Contains(stream, header) {
			t.Errorf("stream %q lacks header %q", stream, header)
		}
	}
}

// The browser's Last-Event-ID wins over the after parameter even when it is
// unusable: a cursor that is not a non-negative number replays the order's
// events from the start instead of skipping any.
func TestV2OrderEventsHandlerReplaysFromTheStartOnAnUnusableLastEventID(t *testing.T) {
	for lastEventID, want := range map[string]int64{
		" 7 ": 7,
		"abc": 0,
		"-3":  0,
		"7.5": 0,
	} {
		svc := &streamService{}
		serveEvents(t, svc, lastEventID)
		if svc.got.AfterID != want {
			t.Errorf("Last-Event-ID %q: replay after %d, want %d", lastEventID, svc.got.AfterID, want)
		}
	}
}

// sessionService answers the session exchange with a token or a refusal.
type sessionService struct {
	billing.Service
	err error
}

func (s sessionService) V2Session(context.Context, string, string) (*dto.V2OrderSessionResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &dto.V2OrderSessionResponse{AccessToken: "session-token"}, nil
}

// An answer carrying a session token must not be cached anywhere.
func TestV2OrderSessionHandlerForbidsCachingTheToken(t *testing.T) {
	serve := func(svc billing.Service) *app.RequestContext {
		engine := server.Default()
		ctx := engine.NewContext()
		ctx.Request.Header.SetMethod(http.MethodPost)
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Request.SetRequestURI("/v2/public/orders/order-1/session")
		ctx.Request.SetBodyString(`{"checkout_token":"capability"}`)
		ctx.Params = append(ctx.Params, param.Param{Key: "orderNo", Value: "order-1"})
		V2OrderSessionHandler(svc)(context.Background(), ctx)
		return ctx
	}
	if ctx := serve(sessionService{}); ctx.Response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(ctx.Response.Body()), "session-token") {
		t.Fatalf("session answer: Cache-Control %q, body %s", ctx.Response.Header.Get("Cache-Control"), ctx.Response.Body())
	}
	if ctx := serve(sessionService{err: xerr.NewErrCode(xerr.InvalidAccess)}); ctx.Response.Header.Get("Cache-Control") != "" {
		t.Fatalf("refusal: Cache-Control %q, want none", ctx.Response.Header.Get("Cache-Control"))
	}
}
