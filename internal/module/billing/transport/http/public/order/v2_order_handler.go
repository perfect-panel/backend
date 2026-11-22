package order

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// EventStreamer is the part of the billing facade the V2 SSE endpoint uses.
type EventStreamer interface {
	V2StreamOrderEvents(ctx context.Context, req billing.V2EventStreamRequest, sink billing.V2EventSink) error
}

// EventStreamDeps contains the dependencies of the V2 SSE endpoint; the
// stream use case lives behind the billing facade.
type EventStreamDeps struct {
	Billing EventStreamer
}

// V2CreateAndCheckoutHandler combines order creation and checkout initiation.
// The idempotency key is intentionally a header so browser retry middleware
// can preserve it independently from a JSON request body.
//
// @Summary Create an order and initiate checkout
// @Tags user
// @Accept json
// @Produce json
// @Param Idempotency-Key header string true "16-128 character request idempotency key"
// @Param request body dto.V2CreateOrderRequest true "Order parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.V2OrderResponse}
// @Failure 409 {object} httpx.ResponseErrorBean
// @Router /v2/public/orders [post]
func V2CreateAndCheckoutHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		idempotencyKey := strings.TrimSpace(string(ctx.GetHeader("Idempotency-Key")))
		if !validIdempotencyKey(idempotencyKey) {
			httpx.ParamErrorResult(ctx, errors.New("Idempotency-Key must contain 16-128 printable ASCII characters"))
			return
		}
		var req dto.V2CreateOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.V2CreateAndCheckout(c, &req, idempotencyKey)
		if errors.Is(err, billing.ErrIdempotencyKeyReused) {
			ctx.JSON(http.StatusConflict, httpx.Error(xerr.InvalidParams, "IDEMPOTENCY_KEY_REUSED"))
			return
		}
		httpx.HttpResult(ctx, resp, err)
	}
}

// @Summary Re-initiate checkout for a pending V2 order
// @Tags user
// @Accept json
// @Produce json
// @Param orderNo path string true "Order number"
// @Param request body dto.V2CheckoutOrderRequest true "Checkout capability and return URL"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.V2OrderResponse}
// @Router /v2/public/orders/{orderNo}/checkout [post]
func V2CheckoutHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.V2CheckoutOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.V2Checkout(c, ctx.Param("orderNo"), &req)
		httpx.HttpResult(ctx, resp, err)
	}
}

// @Summary Get a V2 order state snapshot
// @Tags user
// @Produce json
// @Param orderNo path string true "Order number"
// @Param checkout_token query string false "Guest checkout capability"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.V2OrderResponse}
// @Router /v2/public/orders/{orderNo} [get]
func V2GetOrderHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		resp, err := service.V2GetOrder(c, ctx.Param("orderNo"), ctx.Query("checkout_token"))
		httpx.HttpResult(ctx, resp, err)
	}
}

// @Summary Refresh a V2 order event stream ticket
// @Tags user
// @Accept json
// @Produce json
// @Param orderNo path string true "Order number"
// @Param request body dto.V2EventTicketRequest true "Guest checkout capability when applicable"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.V2EventTicketResponse}
// @Router /v2/public/orders/{orderNo}/event-ticket [post]
func V2EventTicketHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.V2EventTicketRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.V2EventTicket(c, ctx.Param("orderNo"), req.CheckoutToken)
		httpx.HttpResult(ctx, resp, err)
	}
}

// @Summary Exchange a guest checkout capability for a V2 user session
// @Tags user
// @Accept json
// @Produce json
// @Param orderNo path string true "Order number"
// @Param request body dto.V2OrderSessionRequest true "Guest checkout capability"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.V2OrderSessionResponse}
// @Router /v2/public/orders/{orderNo}/session [post]
func V2OrderSessionHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.V2OrderSessionRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.V2Session(c, ctx.Param("orderNo"), req.CheckoutToken)
		if err == nil {
			// The answer carries a session token; no cache may keep it.
			ctx.Header("Cache-Control", "no-store")
		}
		httpx.HttpResult(ctx, resp, err)
	}
}

// V2OrderEventsHandler serves a replayable SSE stream. The billing module
// authorizes the ticket, replays the durable events after the cursor and
// forwards live ones; the handler only adapts the stream to SSE.
//
// @Summary Stream V2 order events
// @Tags user
// @Produce text/event-stream
// @Param orderNo path string true "Order number"
// @Param ticket query string true "Short-lived order event ticket"
// @Param Last-Event-ID header string false "Last received event ID"
// @Param after query string false "Replay cursor when Last-Event-ID is unavailable"
// @Success 200 {string} string
// @Router /v2/public/orders/{orderNo}/events [get]
func V2OrderEventsHandler(deps EventStreamDeps) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		sink := &sseSink{ctx: ctx}
		defer sink.close()
		err := deps.Billing.V2StreamOrderEvents(c, billing.V2EventStreamRequest{
			OrderNo: ctx.Param("orderNo"),
			Ticket:  ctx.Query("ticket"),
			AfterID: requestedEventID(ctx),
		}, sink)
		if err == nil || sink.writer != nil {
			return
		}
		if errors.Is(err, billing.ErrTooManyEventStreams) {
			ctx.JSON(http.StatusTooManyRequests, httpx.Error(xerr.TooManyRequests, "too many concurrent SSE connections"))
			return
		}
		httpx.HttpResult(ctx, nil, err)
	}
}

// sseSink starts the SSE response on the first event, so a refused stream
// can still be answered with a JSON error.
type sseSink struct {
	ctx    *app.RequestContext
	writer *sse.Writer
}

func (s *sseSink) start() *sse.Writer {
	if s.writer == nil {
		s.ctx.Header("X-Accel-Buffering", "no")
		s.ctx.Header("Cache-Control", "no-cache")
		s.writer = sse.NewWriter(s.ctx)
	}
	return s.writer
}

func (s *sseSink) Event(id, name string, data []byte) error {
	return s.start().WriteEvent(id, name, data)
}

func (s *sseSink) KeepAlive() error {
	return s.start().WriteKeepAlive()
}

func (s *sseSink) close() {
	if s.writer != nil {
		_ = s.writer.Close()
	}
}

func validIdempotencyKey(key string) bool {
	if len(key) < 16 || len(key) > 128 {
		return false
	}
	for _, char := range []byte(key) {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func requestedEventID(ctx *app.RequestContext) int64 {
	value := strings.TrimSpace(string(ctx.GetHeader("Last-Event-ID")))
	if value == "" {
		value = strings.TrimSpace(ctx.Query("after"))
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}
