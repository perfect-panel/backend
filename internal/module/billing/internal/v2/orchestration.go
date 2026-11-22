// Package v2 implements the V2 order orchestration subdomain of the billing
// module: idempotent create-and-checkout, guest checkout capabilities, the
// SSE event-stream tickets and the event stream itself. Only the module
// facade may reach it.
package v2

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/auth/token"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ordercontext"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

const (
	v2OrderTypePurchase     = "purchase"
	v2OrderTypeRenewal      = "renewal"
	v2OrderTypeResetTraffic = "reset_traffic"
	v2OrderTypeRecharge     = "recharge"

	v2EventScope       = "order-events:read"
	v2EventTicketExtra = 10 * time.Minute
)

// ErrIdempotencyKeyReused is handled as HTTP 409 by the V2 handler. It is a
// distinct transport condition: the original order remains intact.
var ErrIdempotencyKeyReused = errors.New("idempotency key reused with a different request")

// Orders reads the orders the orchestration creates.
type Orders interface {
	FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error)
	FindOneByIdempotencyKey(ctx context.Context, key string) (*order.Order, error)
}

// ReplayLimiter grants a bounded number of permits per key and period; the
// auth rate limiter provides it, with its permit states.
type ReplayLimiter interface {
	Take(ctx context.Context, key string) (int, error)
}

// GuestReplayLimits bound how often an anonymous create request may be
// replayed under an existing idempotency key. A replay proves the guest
// password against the order, so an unlimited replay is a password oracle
// for whoever holds the key; the limits apply per key and per client IP. A
// nil limiter applies no limit.
type GuestReplayLimits struct {
	PerKey ReplayLimiter
	PerIP  ReplayLimiter
}

// Deps declares the orchestration's dependencies: sibling subdomains are
// invoked directly, never through the facade.
type Deps struct {
	Orders       Orders
	Checkout     *checkout.Service
	Portal       *portal.Service
	JwtSecret    string
	CurrencyUnit func() string
	// GuestReplays bounds the replays of anonymous create requests.
	GuestReplays GuestReplayLimits
	// Stream serves the order event streams.
	Stream StreamDeps
}

// Service is the V2 orchestration.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// CreateAndCheckout is the V2 orchestration boundary. Existing domain
// creators still own pricing, inventory and coupon policy; this method only
// gives them an idempotency context and immediately starts checkout.
func (s *Service) CreateAndCheckout(ctx context.Context, req *dto.V2CreateOrderRequest, idempotencyKey string) (*dto.V2OrderResponse, error) {
	currentUser := currentUser(ctx)
	if err := validateV2CreateRequest(req, currentUser); err != nil {
		return nil, err
	}
	hash, err := requestHash(ctx, req)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid order request")
	}

	orderInfo, err := s.deps.Orders.FindOneByIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		return s.replayExistingCreate(ctx, orderInfo, req, idempotencyKey, hash)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find idempotent order")
	}

	checkoutToken := s.derivedGuestCheckoutToken(idempotencyKey)
	meta := ordercontext.Idempotency{Key: idempotencyKey, Hash: hash}
	if currentUser == nil {
		meta.GuestCheckoutToken = checkoutToken
	}
	orderNo, createdCheckoutToken, err := s.createOrder(ordercontext.WithIdempotency(ctx, meta), req)
	if err != nil {
		// A concurrent request with this key can win after our initial lookup.
		// Its transaction owns all reservations; this attempt rolled back
		// before returning the duplicate-key error.
		existing, findErr := s.deps.Orders.FindOneByIdempotencyKey(ctx, idempotencyKey)
		if findErr == nil {
			return s.replayExistingCreate(ctx, existing, req, idempotencyKey, hash)
		}
		return nil, err
	}
	if createdCheckoutToken != "" {
		checkoutToken = createdCheckoutToken
	}
	orderInfo, err = s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "load created order")
	}
	return s.checkoutResponse(ctx, orderInfo, checkoutToken, req.ReturnURL)
}

// replayExistingCreate answers a create request whose idempotency key
// already produced orderInfo: the same request resumes that order's
// checkout, a different one is refused. An anonymous replay is rate limited
// first: it proves the guest password against the order, and the refusal
// tells a correct guess from a wrong one.
func (s *Service) replayExistingCreate(ctx context.Context, orderInfo *order.Order, req *dto.V2CreateOrderRequest, idempotencyKey, hash string) (*dto.V2OrderResponse, error) {
	if currentUser(ctx) == nil {
		if err := s.allowGuestReplay(ctx, idempotencyKey); err != nil {
			return nil, err
		}
	}
	if !sameIdempotencyHash(orderInfo.IdempotencyHash, hash) {
		return nil, ErrIdempotencyKeyReused
	}
	checkoutToken := s.guestCheckoutToken(idempotencyKey, orderInfo)
	if err := s.authorizeExistingCreate(ctx, orderInfo, req, checkoutToken); err != nil {
		return nil, err
	}
	return s.checkoutResponse(ctx, orderInfo, checkoutToken, req.ReturnURL)
}

// allowGuestReplay takes one permit for the replay from the per-key and the
// per-IP limit; over either, the replay is refused as too many requests. A
// limiter that cannot answer refuses too: an oracle must not open when the
// limit store is down.
func (s *Service) allowGuestReplay(ctx context.Context, idempotencyKey string) error {
	if err := takeReplayPermit(ctx, s.deps.GuestReplays.PerKey, idempotencyKey); err != nil {
		return err
	}
	metadata, _ := requestmeta.From(ctx)
	return takeReplayPermit(ctx, s.deps.GuestReplays.PerIP, metadata.ClientIP)
}

func takeReplayPermit(ctx context.Context, limiter ReplayLimiter, key string) error {
	if limiter == nil || key == "" {
		return nil
	}
	state, err := limiter.Take(ctx, key)
	if err != nil {
		return xerr.Wrapf(err, xerr.TooManyRequests, "the replay limit could not be checked")
	}
	if state == ratelimit.OverQuota || state == ratelimit.Unknown {
		return xerr.Errorf(xerr.TooManyRequests, "too many replays of this order request; retry later")
	}
	return nil
}

// Checkout resumes the payment of a pending order.
func (s *Service) Checkout(ctx context.Context, orderNo string, req *dto.V2CheckoutOrderRequest) (*dto.V2OrderResponse, error) {
	orderInfo, err := s.findOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOrder(ctx, orderInfo, req.CheckoutToken); err != nil {
		return nil, err
	}
	if !order.CanCheckout(orderInfo.Status) {
		return nil, xerr.Errorf(xerr.OrderStatusError, "order is not pending")
	}
	return s.checkoutResponse(ctx, orderInfo, req.CheckoutToken, req.ReturnURL)
}

// GetOrder returns the order's state snapshot with a fresh stream ticket.
func (s *Service) GetOrder(ctx context.Context, orderNo, checkoutToken string) (*dto.V2OrderResponse, error) {
	orderInfo, err := s.findOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOrder(ctx, orderInfo, checkoutToken); err != nil {
		return nil, err
	}
	ticket, expiresAt, err := s.mintEventTicket(ctx, orderInfo, checkoutToken)
	if err != nil {
		return nil, err
	}
	return &dto.V2OrderResponse{
		Order:  s.snapshot(orderInfo),
		Events: eventResponse(orderInfo.OrderNo, ticket, expiresAt),
	}, nil
}

// EventTicket mints a fresh stream ticket for the order.
func (s *Service) EventTicket(ctx context.Context, orderNo, checkoutToken string) (*dto.V2EventTicketResponse, error) {
	orderInfo, err := s.findOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOrder(ctx, orderInfo, checkoutToken); err != nil {
		return nil, err
	}
	ticket, expiresAt, err := s.mintEventTicket(ctx, orderInfo, checkoutToken)
	if err != nil {
		return nil, err
	}
	return &dto.V2EventTicketResponse{
		URL:             eventResponse(orderNo, ticket, expiresAt).URL,
		TicketExpiresAt: expiresAt,
	}, nil
}

// Session exchanges the durable guest checkout capability for an ordinary
// session after activation has created the account, within the window and
// under the revocation rule the storefront applies.  It is intentionally a
// separate JSON endpoint: a long-lived access token must never appear in a
// browser-visible EventSource URL or SSE event payload.
func (s *Service) Session(ctx context.Context, orderNo, checkoutToken string) (*dto.V2OrderSessionResponse, error) {
	orderInfo, err := s.findOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOrder(ctx, orderInfo, checkoutToken); err != nil {
		return nil, err
	}
	if orderInfo.GuestCheckoutTokenHash == "" {
		return nil, xerr.Errorf(xerr.InvalidAccess, "order does not have a guest checkout capability")
	}
	// The storefront owns the exchange rule (settlement window, revocation
	// since settlement), shared with V1's status endpoint.
	accessToken, err := s.deps.Portal.ExchangeGuestSession(ctx, orderInfo)
	if err != nil {
		return nil, err
	}
	return &dto.V2OrderSessionResponse{AccessToken: accessToken}, nil
}

// AuthorizeEventStream validates the self-contained stream capability and
// returns the stream's initial snapshot together with the ticket expiry; the
// order entity never leaves the module.
func (s *Service) AuthorizeEventStream(ctx context.Context, orderNo, ticket string) (dto.V2OrderSnapshot, time.Time, error) {
	orderInfo, err := s.authorizeEventTicket(ctx, orderNo, ticket)
	if err != nil {
		return dto.V2OrderSnapshot{}, time.Time{}, err
	}
	expiresAt, err := s.eventTicketExpiresAt(ticket)
	if err != nil {
		return dto.V2OrderSnapshot{}, time.Time{}, err
	}
	return s.snapshot(orderInfo), expiresAt, nil
}

func (s *Service) findOrder(ctx context.Context, orderNo string) (*order.Order, error) {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Errorf(xerr.OrderNotExist, "order not found")
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find order %s", orderNo)
	}
	return orderInfo, nil
}

func (s *Service) createOrder(ctx context.Context, req *dto.V2CreateOrderRequest) (orderNo, checkoutToken string, err error) {
	switch req.Type {
	case v2OrderTypePurchase:
		if currentUser(ctx) == nil {
			resp, e := s.deps.Portal.Purchase(ctx, &dto.PortalPurchaseRequest{
				AuthType: req.Guest.AuthType, Identifier: req.Guest.Identifier, Password: req.Guest.Password,
				Payment: req.PaymentID, SubscribeId: req.SubscribeID, Quantity: req.Quantity,
				Coupon: req.Coupon, InviteCode: req.Guest.InviteCode, TurnstileToken: req.Guest.TurnstileToken,
			})
			if e != nil {
				return "", "", e
			}
			return resp.OrderNo, resp.CheckoutToken, nil
		}
		resp, e := s.deps.Checkout.Purchase(ctx, &dto.PurchaseOrderRequest{
			SubscribeId: req.SubscribeID, Quantity: req.Quantity, Payment: req.PaymentID, Coupon: req.Coupon,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	case v2OrderTypeRenewal:
		resp, e := s.deps.Checkout.Renewal(ctx, &dto.RenewalOrderRequest{
			UserSubscribeID: req.UserSubscribeID, Quantity: req.Quantity, Payment: req.PaymentID, Coupon: req.Coupon,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	case v2OrderTypeResetTraffic:
		resp, e := s.deps.Checkout.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{
			UserSubscribeID: req.UserSubscribeID, Payment: req.PaymentID,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	case v2OrderTypeRecharge:
		resp, e := s.deps.Checkout.Recharge(ctx, &dto.RechargeOrderRequest{
			Amount: req.Amount, Payment: req.PaymentID,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	default:
		return "", "", xerr.Errorf(xerr.InvalidParams, "unsupported order type")
	}
}

// checkoutResponse starts or resumes the payment of a pending order and
// describes the order with a fresh stream ticket. A settled or closed order
// is described without starting a payment.
func (s *Service) checkoutResponse(ctx context.Context, orderInfo *order.Order, checkoutToken, returnURL string) (*dto.V2OrderResponse, error) {
	var paymentResp *dto.V2OrderPayment
	if order.CanCheckout(orderInfo.Status) {
		checkout, err := s.deps.Portal.Checkout(ctx, &dto.CheckoutOrderRequest{
			OrderNo: orderInfo.OrderNo, CheckoutToken: checkoutToken, ReturnUrl: returnURL,
		})
		if err != nil {
			return nil, err
		}
		paymentResp = &dto.V2OrderPayment{
			Type: checkout.Type, CheckoutURL: checkout.CheckoutUrl, Stripe: checkout.Stripe,
			PaymentStatus: order.PaymentStatusName(orderInfo.Status),
		}
		if latest, err := s.deps.Orders.FindOneByOrderNo(ctx, orderInfo.OrderNo); err == nil {
			orderInfo = latest
			paymentResp.PaymentStatus = order.PaymentStatusName(orderInfo.Status)
		}
	}
	ticket, expiresAt, err := s.mintEventTicket(ctx, orderInfo, checkoutToken)
	if err != nil {
		return nil, err
	}
	return &dto.V2OrderResponse{
		Order:         s.snapshot(orderInfo),
		Payment:       paymentResp,
		Events:        eventResponse(orderInfo.OrderNo, ticket, expiresAt),
		CheckoutToken: checkoutTokenForResponse(orderInfo, checkoutToken),
	}, nil
}

func (s *Service) snapshot(orderInfo *order.Order) dto.V2OrderSnapshot {
	currency := ""
	if s.deps.CurrencyUnit != nil {
		currency = s.deps.CurrencyUnit()
	}
	return dto.V2OrderSnapshot{
		OrderNo:           orderInfo.OrderNo,
		Status:            order.StatusName(orderInfo.Status),
		PaymentStatus:     order.PaymentStatusName(orderInfo.Status),
		FulfillmentStatus: order.FulfillmentStatusName(orderInfo.Status),
		StateVersion:      orderInfo.StateVersion,
		Amount:            orderInfo.Amount,
		Currency:          currency,
		ExpiresAt:         orderInfo.CreatedAt.Add(order.PaymentWindow).Unix(),
	}
}

func eventResponse(orderNo, ticket string, expiresAt int64) dto.V2OrderEvents {
	return dto.V2OrderEvents{
		URL:             fmt.Sprintf("/v2/public/orders/%s/events?ticket=%s", url.PathEscape(orderNo), url.QueryEscape(ticket)),
		TicketExpiresAt: expiresAt,
	}
}

// authorizeExistingCreate binds a replayed create request to the order its
// idempotency key already produced. The guest password is kept out of the
// request hash (see requestHash), so a guest replay proves it against the
// stored hash instead; a different password is a different request and gets
// the same refusal as a changed body.
func (s *Service) authorizeExistingCreate(ctx context.Context, orderInfo *order.Order, req *dto.V2CreateOrderRequest, checkoutToken string) error {
	if currentUser(ctx) != nil {
		return s.authorizeOrder(ctx, orderInfo, "")
	}
	if req.Guest == nil || orderInfo.GuestAuthType != req.Guest.AuthType || orderInfo.GuestIdentifier != req.Guest.Identifier {
		return xerr.Errorf(xerr.InvalidAccess, "order does not belong to this checkout")
	}
	if req.Guest.Password != "" || orderInfo.GuestPasswordHash != "" {
		if !password.VerifyPassWord(req.Guest.Password, orderInfo.GuestPasswordHash) {
			return ErrIdempotencyKeyReused
		}
	}
	return s.authorizeOrder(ctx, orderInfo, checkoutToken)
}

func (s *Service) authorizeOrder(ctx context.Context, orderInfo *order.Order, checkoutToken string) error {
	if u := currentUser(ctx); orderInfo.UserId != 0 && u != nil && u.Id == orderInfo.UserId {
		return nil
	}
	if guestCheckoutTokenMatches(orderInfo, checkoutToken) {
		return nil
	}
	return xerr.Errorf(xerr.InvalidAccess, "order does not belong to the current user")
}

func (s *Service) mintEventTicket(ctx context.Context, orderInfo *order.Order, checkoutToken string) (string, int64, error) {
	if err := s.authorizeOrder(ctx, orderInfo, checkoutToken); err != nil {
		return "", 0, err
	}
	expiresAt := orderInfo.CreatedAt.Add(order.PaymentWindow + v2EventTicketExtra)
	if expiresAt.Before(time.Now()) {
		expiresAt = time.Now().Add(v2EventTicketExtra)
	}
	seconds := max(int64(time.Until(expiresAt).Seconds()), 1)
	ticket, err := token.NewJwtToken(s.deps.JwtSecret, time.Now().Unix(), seconds,
		token.WithOption("OrderNo", orderInfo.OrderNo),
		token.WithOption("Scope", v2EventScope),
		token.WithOption("UserId", orderInfo.UserId),
		token.WithOption("GuestCheckoutHash", orderInfo.GuestCheckoutTokenHash),
	)
	if err != nil {
		return "", 0, xerr.Wrapf(err, xerr.ERROR, "create event ticket")
	}
	return ticket, expiresAt.Unix(), nil
}

// authorizeEventTicket validates the self-contained stream capability against
// the current order row. It deliberately does not require a long-lived bearer
// token in the EventSource URL.
func (s *Service) authorizeEventTicket(ctx context.Context, orderNo, ticket string) (*order.Order, error) {
	claims, err := token.ParseJwtToken(ticket, s.deps.JwtSecret)
	if err != nil || claimString(claims, "OrderNo") != orderNo || claimString(claims, "Scope") != v2EventScope {
		return nil, xerr.Errorf(xerr.InvalidAccess, "event ticket is invalid")
	}
	orderInfo, err := s.findOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if guestCheckoutHashMatches(orderInfo, claimString(claims, "GuestCheckoutHash")) {
		return orderInfo, nil
	}
	if orderInfo.UserId == 0 || claimInt64(claims, "UserId") != orderInfo.UserId {
		return nil, xerr.Errorf(xerr.InvalidAccess, "event ticket is invalid")
	}
	return orderInfo, nil
}

func (s *Service) eventTicketExpiresAt(ticket string) (time.Time, error) {
	claims, err := token.ParseJwtToken(ticket, s.deps.JwtSecret)
	if err != nil {
		return time.Time{}, xerr.Errorf(xerr.InvalidAccess, "event ticket is invalid")
	}
	expiresAt := claimInt64(claims, "exp")
	if expiresAt <= time.Now().Unix() {
		return time.Time{}, xerr.Errorf(xerr.InvalidAccess, "event ticket expired")
	}
	return time.Unix(expiresAt, 0), nil
}

func currentUser(ctx context.Context) *user.User {
	u, _ := user.FromContext(ctx)
	return u
}

// requestHash is the stable identity of a create request under its
// idempotency key. It is stored on the order row, so it carries nothing
// secret: the guest password stays out (a stored unsalted digest of it would
// let anyone reading the row test guesses at hash speed) and is proved
// against the order's password hash on a replay instead.
func requestHash(ctx context.Context, req *dto.V2CreateOrderRequest) (string, error) {
	canonical := struct {
		Type            string
		PaymentID       int64
		SubscribeID     int64
		UserSubscribeID int64
		Quantity        int64
		Coupon          string
		Amount          int64
		UserID          int64
		Guest           *dto.V2GuestOrderRequest
	}{
		Type: req.Type, PaymentID: req.PaymentID, SubscribeID: req.SubscribeID,
		UserSubscribeID: req.UserSubscribeID, Quantity: req.Quantity, Coupon: req.Coupon,
		Amount: req.Amount,
	}
	if u := currentUser(ctx); u != nil {
		canonical.UserID = u.Id
	} else if req.Guest != nil {
		// A Turnstile token is single-use, so a retry carries a fresh one; it
		// must not change the request identity either.
		guest := *req.Guest
		guest.TurnstileToken = ""
		guest.Password = ""
		canonical.Guest = &guest
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) derivedGuestCheckoutToken(idempotencyKey string) string {
	mac := hmac.New(sha256.New, []byte(s.deps.JwtSecret))
	_, _ = mac.Write([]byte("v2-guest-checkout:" + idempotencyKey))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) guestCheckoutToken(idempotencyKey string, orderInfo *order.Order) string {
	if orderInfo.GuestCheckoutTokenHash == "" {
		return ""
	}
	derived := s.derivedGuestCheckoutToken(idempotencyKey)
	if !guestCheckoutTokenMatches(orderInfo, derived) {
		return ""
	}
	return derived
}

func validateV2CreateRequest(req *dto.V2CreateOrderRequest, currentUser *user.User) error {
	if req == nil || req.PaymentID <= 0 {
		return xerr.Errorf(xerr.InvalidParams, "payment_id is required")
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	switch req.Type {
	case v2OrderTypePurchase:
		if req.SubscribeID <= 0 || req.Quantity <= 0 || req.Quantity > checkout.MaxQuantity || req.UserSubscribeID != 0 || req.Amount != 0 {
			return xerr.Errorf(xerr.InvalidParams, "invalid purchase parameters")
		}
		if currentUser == nil {
			if req.Guest == nil || len(req.Guest.Password) < 8 || len(req.Guest.Password) > 128 {
				return xerr.Errorf(xerr.InvalidParams, "guest credentials are required")
			}
			// Canonicalize in place so the idempotency hash, the replay
			// ownership check and the created order share one identity.
			authType, identifier, err := portal.NormalizeGuestIdentity(req.Guest.AuthType, req.Guest.Identifier)
			if err != nil {
				return err
			}
			req.Guest.AuthType, req.Guest.Identifier = authType, identifier
		} else if req.Guest != nil {
			return xerr.Errorf(xerr.InvalidParams, "guest is only allowed for anonymous purchase")
		}
	case v2OrderTypeRenewal:
		if currentUser == nil || req.UserSubscribeID <= 0 || req.Quantity <= 0 || req.Quantity > checkout.MaxQuantity || req.SubscribeID != 0 || req.Amount != 0 || req.Guest != nil {
			return xerr.Errorf(xerr.InvalidParams, "invalid renewal parameters")
		}
	case v2OrderTypeResetTraffic:
		if currentUser == nil || req.UserSubscribeID <= 0 || req.SubscribeID != 0 || req.Quantity != 0 || req.Amount != 0 || req.Coupon != "" || req.Guest != nil {
			return xerr.Errorf(xerr.InvalidParams, "invalid reset traffic parameters")
		}
	case v2OrderTypeRecharge:
		if currentUser == nil || req.Amount <= 0 || req.SubscribeID != 0 || req.UserSubscribeID != 0 || req.Quantity != 0 || req.Coupon != "" || req.Guest != nil {
			return xerr.Errorf(xerr.InvalidParams, "invalid recharge parameters")
		}
	default:
		return xerr.Errorf(xerr.InvalidParams, "unsupported order type")
	}
	return nil
}

func sameIdempotencyHash(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func checkoutTokenForResponse(orderInfo *order.Order, checkoutToken string) string {
	if guestCheckoutTokenMatches(orderInfo, checkoutToken) {
		return checkoutToken
	}
	return ""
}

func guestCheckoutTokenMatches(orderInfo *order.Order, checkoutToken string) bool {
	if checkoutToken == "" || orderInfo.GuestCheckoutTokenHash == "" {
		return false
	}
	return guestCheckoutHashMatches(orderInfo, order.CheckoutTokenHash(checkoutToken))
}

func guestCheckoutHashMatches(orderInfo *order.Order, checkoutHash string) bool {
	return checkoutHash != "" && orderInfo.GuestCheckoutTokenHash != "" &&
		subtle.ConstantTimeCompare([]byte(orderInfo.GuestCheckoutTokenHash), []byte(checkoutHash)) == 1
}

func claimString(claims map[string]any, name string) string {
	value, _ := claims[name].(string)
	return value
}

func claimInt64(claims map[string]any, name string) int64 {
	switch value := claims[name].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case json.Number:
		result, _ := value.Int64()
		return result
	default:
		return 0
	}
}
