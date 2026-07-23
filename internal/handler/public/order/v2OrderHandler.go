package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stdErrors "errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
	orderLogic "github.com/perfect-panel/server/internal/logic/public/order"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

const v2SSEMaxConnectionsPerTicket = 3

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
// @Success 200 {object} result.ResponseSuccessBean{data=dto.V2OrderResponse}
// @Failure 409 {object} result.ResponseErrorBean
// @Router /v2/public/orders [post]
func V2CreateAndCheckoutHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		idempotencyKey := strings.TrimSpace(string(ctx.GetHeader("Idempotency-Key")))
		if !validIdempotencyKey(idempotencyKey) {
			result.ParamErrorResult(ctx, stdErrors.New("Idempotency-Key must contain 16-128 printable ASCII characters"))
			return
		}
		var req dto.V2CreateOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c, svcCtx).CreateAndCheckout(&req, idempotencyKey)
		if stdErrors.Is(err, orderLogic.ErrIdempotencyKeyReused) {
			ctx.JSON(http.StatusConflict, result.Error(xerr.InvalidParams, "IDEMPOTENCY_KEY_REUSED"))
			return
		}
		result.HttpResult(ctx, resp, err)
	}
}

// @Summary Re-initiate checkout for a pending V2 order
// @Tags user
// @Accept json
// @Produce json
// @Param orderNo path string true "Order number"
// @Param request body dto.V2CheckoutOrderRequest true "Checkout capability and return URL"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.V2OrderResponse}
// @Router /v2/public/orders/{orderNo}/checkout [post]
func V2CheckoutHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.V2CheckoutOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c, svcCtx).Checkout(ctx.Param("orderNo"), &req)
		result.HttpResult(ctx, resp, err)
	}
}

// @Summary Get a V2 order state snapshot
// @Tags user
// @Produce json
// @Param orderNo path string true "Order number"
// @Param checkout_token query string false "Guest checkout capability"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.V2OrderResponse}
// @Router /v2/public/orders/{orderNo} [get]
func V2GetOrderHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		resp, err := orderLogic.NewV2OrderLogic(c, svcCtx).GetOrder(ctx.Param("orderNo"), ctx.Query("checkout_token"))
		result.HttpResult(ctx, resp, err)
	}
}

// @Summary Refresh a V2 order event stream ticket
// @Tags user
// @Accept json
// @Produce json
// @Param orderNo path string true "Order number"
// @Param request body dto.V2EventTicketRequest true "Guest checkout capability when applicable"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.V2EventTicketResponse}
// @Router /v2/public/orders/{orderNo}/event-ticket [post]
func V2EventTicketHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.V2EventTicketRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c, svcCtx).EventTicket(ctx.Param("orderNo"), req.CheckoutToken)
		result.HttpResult(ctx, resp, err)
	}
}

// @Summary Exchange a guest checkout capability for a V2 user session
// @Tags user
// @Accept json
// @Produce json
// @Param orderNo path string true "Order number"
// @Param request body dto.V2OrderSessionRequest true "Guest checkout capability"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.V2OrderSessionResponse}
// @Router /v2/public/orders/{orderNo}/session [post]
func V2OrderSessionHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.V2OrderSessionRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c, svcCtx).Session(ctx.Param("orderNo"), req.CheckoutToken)
		result.HttpResult(ctx, resp, err)
	}
}

// V2OrderEventsHandler serves a replayable SSE stream. The event table is the
// source of truth; Redis is only used to wake the handler quickly after an
// outbox publication. A periodic database catch-up keeps streams correct if
// Redis or a subscription is briefly unavailable.
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
func V2OrderEventsHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		logic := orderLogic.NewV2OrderLogic(c, svcCtx)
		orderNo := ctx.Param("orderNo")
		ticket := ctx.Query("ticket")
		orderInfo, err := logic.AuthorizeEventTicket(orderNo, ticket)
		if err != nil {
			result.HttpResult(ctx, nil, err)
			return
		}
		expiresAt, err := logic.EventTicketExpiresAt(ticket)
		if err != nil {
			result.HttpResult(ctx, nil, err)
			return
		}
		release, allowed := acquireSSEConnection(c, svcCtx.Redis, ticket, time.Until(expiresAt))
		if !allowed {
			ctx.JSON(http.StatusTooManyRequests, result.Error(xerr.TooManyRequests, "too many concurrent SSE connections"))
			return
		}
		defer release()

		ctx.Header("X-Accel-Buffering", "no")
		ctx.Header("Cache-Control", "no-cache")
		writer := sse.NewWriter(ctx)
		defer func() { _ = writer.Close() }()

		// Subscribe before querying the event table. Query and broadcast can
		// overlap, but the monotonically increasing event id makes that safe.
		pubsub, messages := subscribeOrderEvents(c, svcCtx.Redis, orderNo)
		if pubsub != nil {
			defer func() { _ = pubsub.Close() }()
		}

		if err := writeSSESnapshot(writer, logic.Snapshot(orderInfo)); err != nil {
			return
		}
		afterID := requestedEventID(ctx)
		if afterID > 0 {
			earliestID, err := svcCtx.Store.OrderEvent().EarliestID(c, orderNo)
			if err != nil {
				logger.WithContext(c).Errorw("[V2OrderEvents] inspect replay cursor failed", logger.Field("error", err.Error()), logger.Field("order_no", orderNo))
			} else if earliestID > afterID {
				if err := writeSSEReset(writer, logic.Snapshot(orderInfo)); err != nil {
					return
				}
				afterID = earliestID - 1
			}
		}
		if err := replayOrderEvents(c, writer, svcCtx, orderNo, &afterID); err != nil {
			logger.WithContext(c).Errorw("[V2OrderEvents] initial replay failed", logger.Field("error", err.Error()), logger.Field("order_no", orderNo))
		}

		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		catchUp := time.NewTicker(5 * time.Second)
		defer catchUp.Stop()
		resubscribe := time.NewTicker(5 * time.Second)
		defer resubscribe.Stop()
		expiration := time.NewTimer(time.Until(expiresAt))
		defer expiration.Stop()

		for {
			select {
			case <-c.Done():
				return
			case <-expiration.C:
				data, _ := json.Marshal(map[string]string{"reason": "ticket_expired"})
				_ = writer.WriteEvent("", "stream.expiring", data)
				return
			case _, ok := <-messages:
				if !ok {
					messages = nil
					if pubsub != nil {
						_ = pubsub.Close()
						pubsub = nil
					}
					continue
				}
				if err := replayOrderEvents(c, writer, svcCtx, orderNo, &afterID); err != nil {
					return
				}
			case <-catchUp.C:
				if err := replayOrderEvents(c, writer, svcCtx, orderNo, &afterID); err != nil {
					return
				}
			case <-heartbeat.C:
				if err := writer.WriteKeepAlive(); err != nil {
					return
				}
			case <-resubscribe.C:
				if messages == nil {
					pubsub, messages = subscribeOrderEvents(c, svcCtx.Redis, orderNo)
				}
			}
		}
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

func writeSSESnapshot(writer *sse.Writer, snapshot dto.V2OrderSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return writer.WriteEvent("", "order.snapshot", data)
}

func writeSSEReset(writer *sse.Writer, snapshot dto.V2OrderSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return writer.WriteEvent("", "order.reset", data)
}

func replayOrderEvents(ctx context.Context, writer *sse.Writer, svcCtx *svc.ServiceContext, orderNo string, afterID *int64) error {
	for {
		events, err := svcCtx.Store.OrderEvent().ListAfter(ctx, orderNo, *afterID, 500)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.ID <= *afterID {
				continue
			}
			if err := writer.WriteEvent(strconv.FormatInt(event.ID, 10), event.EventType, []byte(event.Payload)); err != nil {
				return err
			}
			*afterID = event.ID
		}
		if len(events) < 500 {
			return nil
		}
	}
}

func subscribeOrderEvents(ctx context.Context, client *redis.Client, orderNo string) (*redis.PubSub, <-chan *redis.Message) {
	if client == nil {
		return nil, nil
	}
	pubsub := client.Subscribe(ctx, orderstream.Channel(orderNo))
	confirmCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := pubsub.Receive(confirmCtx); err != nil {
		_ = pubsub.Close()
		return nil, nil
	}
	return pubsub, pubsub.Channel()
}

func acquireSSEConnection(ctx context.Context, client *redis.Client, ticket string, ttl time.Duration) (func(), bool) {
	if client == nil {
		return func() {}, true
	}
	digest := sha256.Sum256([]byte(ticket))
	key := "order:sse:connections:" + hex.EncodeToString(digest[:])
	count, err := client.Incr(ctx, key).Result()
	if err != nil {
		// The event table can still sustain an SSE connection during a Redis
		// outage. The expiry guard is a best-effort abuse control, not a reason
		// to hide a paid order from its owner.
		return func() {}, true
	}
	if count == 1 {
		if ttl < time.Minute {
			ttl = time.Minute
		}
		_ = client.Expire(ctx, key, ttl).Err()
	}
	if count > v2SSEMaxConnectionsPerTicket {
		_, _ = client.Decr(ctx, key).Result()
		return func() {}, false
	}
	return func() {
		_, _ = client.Decr(context.Background(), key).Result()
	}, true
}
