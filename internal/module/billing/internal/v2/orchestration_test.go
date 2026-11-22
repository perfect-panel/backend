package v2

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/auth/token"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ticketOrders holds the one order a ticket test changes in place.
type ticketOrders struct {
	order *order.Order
}

var _ Orders = (*ticketOrders)(nil)

func (r *ticketOrders) FindOneByOrderNo(_ context.Context, orderNo string) (*order.Order, error) {
	if r.order == nil || r.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.order
	return &copy, nil
}

func (r *ticketOrders) FindOneByIdempotencyKey(context.Context, string) (*order.Order, error) {
	return nil, gorm.ErrRecordNotFound
}

func userContext(id int64) context.Context {
	return user.NewContext(context.Background(), &user.User{Id: id})
}

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

func TestV2OrderRequestHashIgnoresReturnURLAndBindsUser(t *testing.T) {
	ctx := userContext(17)
	first := &dto.V2CreateOrderRequest{
		Type: v2OrderTypePurchase, PaymentID: 3, SubscribeID: 9, Quantity: 2, Coupon: "SUMMER",
		ReturnURL: "https://one.example/result",
	}
	second := *first
	second.ReturnURL = "https://two.example/result"
	firstHash, err := requestHash(ctx, first)
	if err != nil {
		t.Fatalf("hash first request: %v", err)
	}
	secondHash, err := requestHash(ctx, &second)
	if err != nil {
		t.Fatalf("hash second request: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("return_url changed stable hash: %s != %s", firstHash, secondHash)
	}
	second.Coupon = "OTHER"
	if changed, _ := requestHash(ctx, &second); changed == firstHash {
		t.Fatal("business request change must change idempotency hash")
	}
	if otherUser, _ := requestHash(userContext(18), first); otherUser == firstHash {
		t.Fatal("the idempotency hash must bind the user")
	}
}

func TestV2GuestCheckoutTokenIsDeterministicPerIdempotencyKey(t *testing.T) {
	svc := NewService(Deps{JwtSecret: "stream-secret"})
	first := svc.derivedGuestCheckoutToken("1234567890abcdef")
	second := svc.derivedGuestCheckoutToken("1234567890abcdef")
	third := svc.derivedGuestCheckoutToken("abcdef1234567890")
	if first == "" || first != second || first == third {
		t.Fatalf("derived guest capability is not deterministic and key-bound")
	}
}

func TestV2OrderEventTicketBindsCurrentOrderOwner(t *testing.T) {
	orderInfo := &order.Order{OrderNo: "order-ticket", UserId: 17, Status: order.StatusPending, CreatedAt: time.Now(), StateVersion: 1}
	ctx := userContext(17)
	svc := NewService(Deps{JwtSecret: "stream-secret", Orders: &ticketOrders{order: orderInfo}})
	ticket, _, err := svc.mintEventTicket(ctx, orderInfo, "")
	if err != nil {
		t.Fatalf("mint ticket: %v", err)
	}
	claimed, err := svc.authorizeEventTicket(ctx, orderInfo.OrderNo, ticket)
	if err != nil || claimed.OrderNo != orderInfo.OrderNo {
		t.Fatalf("authorize ticket = (%v, %v)", claimed, err)
	}
	_, err = svc.authorizeEventTicket(ctx, "other-order", ticket)
	assertCode(t, err, xerr.InvalidAccess)
	if _, _, err := svc.mintEventTicket(userContext(18), orderInfo, ""); err == nil {
		t.Fatal("another user minted a ticket for the order")
	}
	expired, err := token.NewJwtToken("stream-secret", time.Now().Add(-time.Minute).Unix(), 1,
		token.WithOption("OrderNo", orderInfo.OrderNo), token.WithOption("Scope", v2EventScope), token.WithOption("UserId", orderInfo.UserId))
	if err != nil {
		t.Fatalf("mint expired ticket: %v", err)
	}
	if _, err := svc.eventTicketExpiresAt(expired); err == nil {
		t.Fatal("expired stream ticket must be rejected")
	}
}

func TestV2GuestCapabilitySurvivesAccountActivation(t *testing.T) {
	const guestCapability = "guest-checkout-capability"
	orderInfo := &order.Order{
		OrderNo: "guest-order", Status: order.StatusPaid, CreatedAt: time.Now(), StateVersion: 2,
		GuestAuthType: "email", GuestIdentifier: "guest@example.com",
		GuestCheckoutTokenHash: order.CheckoutTokenHash(guestCapability),
	}
	orders := &ticketOrders{order: orderInfo}
	ctx := context.Background()
	svc := NewService(Deps{JwtSecret: "stream-secret", Orders: orders})

	ticket, _, err := svc.mintEventTicket(ctx, orderInfo, guestCapability)
	if err != nil {
		t.Fatalf("mint guest ticket: %v", err)
	}
	// Activation creates the user after payment, while the browser may hold
	// an already-issued stream ticket and a persisted checkout capability.
	orders.order.UserId = 42
	orders.order.Status = order.StatusFinished

	if _, err := svc.authorizeEventTicket(ctx, orderInfo.OrderNo, ticket); err != nil {
		t.Fatalf("pre-activation guest ticket must reconnect after account creation: %v", err)
	}
	if _, err := svc.EventTicket(ctx, orderInfo.OrderNo, guestCapability); err != nil {
		t.Fatalf("guest capability must refresh ticket after account creation: %v", err)
	}
	orders.order.GuestCheckoutTokenHash = order.CheckoutTokenHash("replaced-capability")
	if _, err := svc.authorizeEventTicket(ctx, orderInfo.OrderNo, ticket); err == nil {
		t.Fatal("ticket must be rejected when its guest capability is no longer valid")
	}
	orders.order.GuestCheckoutTokenHash = order.CheckoutTokenHash(guestCapability)
	if err := svc.authorizeExistingCreate(ctx, orders.order, &dto.V2CreateOrderRequest{
		Type:  v2OrderTypePurchase,
		Guest: &dto.V2GuestOrderRequest{AuthType: "email", Identifier: "guest@example.com"},
	}, guestCapability); err != nil {
		t.Fatalf("idempotent guest recovery must remain authorized after account creation: %v", err)
	}
	if got := checkoutTokenForResponse(orders.order, guestCapability); got != guestCapability {
		t.Fatalf("checkout capability = %q, want it retained for recovery", got)
	}
}

// settledEvents dates one order's payment event; a zero time is an order
// without one.
type settledEvents struct {
	orderNo string
	at      time.Time
}

func (e *settledEvents) ListAfter(_ context.Context, orderNo string, _ int64, _ int) ([]*order.Event, error) {
	if orderNo != e.orderNo || e.at.IsZero() {
		return nil, nil
	}
	return []*order.Event{{OrderNo: orderNo, EventType: order.EventTypePaymentPaid, CreatedAt: e.at}}, nil
}

// The V2 session endpoint follows the storefront's exchange rule: the
// account must exist, the settlement must be within the window, and the
// account's sessions must not have been revoked since.
func TestV2GuestSessionExchangeRequiresActivatedAccount(t *testing.T) {
	const guestCapability = "guest-checkout-capability"
	orderInfo := &order.Order{
		OrderNo: "guest-session", Status: order.StatusPaid, CreatedAt: time.Now(), StateVersion: 2,
		GuestCheckoutTokenHash: order.CheckoutTokenHash(guestCapability),
	}
	orders := &ticketOrders{order: orderInfo}
	events := &settledEvents{orderNo: orderInfo.OrderNo, at: time.Now()}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	svc := NewService(Deps{
		JwtSecret: "session-secret",
		Orders:    orders,
		Portal: portal.NewService(portal.Deps{
			Sessions:    redisClient,
			OrderEvents: events,
			Config:      portal.Config{JwtSecret: "session-secret", JwtExpire: 3600},
		}),
	})
	ctx := context.Background()

	_, err := svc.Session(ctx, orderInfo.OrderNo, guestCapability)
	assertCode(t, err, xerr.OrderStatusError)
	orders.order.UserId = 42
	response, err := svc.Session(ctx, orderInfo.OrderNo, guestCapability)
	if err != nil {
		t.Fatalf("exchange guest capability for session: %v", err)
	}
	claims, err := token.ParseJwtToken(response.AccessToken, "session-secret")
	if err != nil || claimInt64(claims, "UserId") != 42 {
		t.Fatalf("session claims = %#v, %v; want user 42", claims, err)
	}
	sessionID, _ := claims["SessionId"].(string)
	storedUserID, err := redisClient.Get(ctx, fmt.Sprintf("%v:%v", config.SessionIdKey, sessionID)).Result()
	if err != nil || storedUserID != "42" {
		t.Fatalf("session cache = (%q, %v), want user 42", storedUserID, err)
	}
	_, err = svc.Session(ctx, orderInfo.OrderNo, "incorrect-capability")
	assertCode(t, err, xerr.InvalidAccess)

	// The capability is durable; the exchange is not.
	events.at = time.Now().Add(-portal.GuestSessionExchangeWindow - time.Minute)
	_, err = svc.Session(ctx, orderInfo.OrderNo, guestCapability)
	assertCode(t, err, xerr.InvalidAccess)
	events.at = time.Time{}
	_, err = svc.Session(ctx, orderInfo.OrderNo, guestCapability)
	assertCode(t, err, xerr.InvalidAccess)

	// A password change or reset since the settlement ends the exchange too.
	events.at = time.Now()
	if err := usersession.Revoke(ctx, redisClient, 42); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Session(ctx, orderInfo.OrderNo, guestCapability)
	assertCode(t, err, xerr.InvalidAccess)
}

func v2GuestPurchase(authType, identifier string) *dto.V2CreateOrderRequest {
	return &dto.V2CreateOrderRequest{
		Type: v2OrderTypePurchase, PaymentID: 2, SubscribeID: 9, Quantity: 1,
		Guest: &dto.V2GuestOrderRequest{AuthType: authType, Identifier: identifier, Password: "guest-password"},
	}
}

// An anonymous purchase must not bind an OAuth or device identity: the paid
// order would insert it as given and issue a session for it.
func TestValidateV2CreateRequestRejectsNonPasswordGuestAuthTypes(t *testing.T) {
	for _, authType := range []string{"github", "telegram", "google", "device", ""} {
		assertCode(t, validateV2CreateRequest(v2GuestPurchase(authType, "123456789"), nil), xerr.InvalidParams)
	}
	assertCode(t, validateV2CreateRequest(v2GuestPurchase("email", "not-an-email"), nil), xerr.InvalidParams)
}

func TestValidateV2CreateRequestCanonicalizesGuestIdentity(t *testing.T) {
	req := v2GuestPurchase(" Email ", "  Guest@Example.COM ")
	if err := validateV2CreateRequest(req, nil); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if req.Guest.AuthType != "email" || req.Guest.Identifier != "guest@example.com" {
		t.Fatalf("guest identity = (%q, %q), want canonical email", req.Guest.AuthType, req.Guest.Identifier)
	}
	req = v2GuestPurchase("mobile", "+86 155 0250 5555")
	if err := validateV2CreateRequest(req, nil); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if req.Guest.Identifier != "+8615502505555" {
		t.Fatalf("guest mobile = %q, want E.164", req.Guest.Identifier)
	}
}

// The hash is stored on the order row, so it must not let a reader test
// password guesses: two guest requests that differ only in the password
// hash the same, and the password stays in the request for the replay check.
func TestV2GuestRequestHashCarriesNoPassword(t *testing.T) {
	ctx := context.Background()
	first := v2GuestPurchase("email", "guest@example.com")
	second := v2GuestPurchase("email", "guest@example.com")
	second.Guest.Password = "another-password"
	firstHash, err := requestHash(ctx, first)
	if err != nil {
		t.Fatalf("hash first request: %v", err)
	}
	secondHash, err := requestHash(ctx, second)
	if err != nil {
		t.Fatalf("hash second request: %v", err)
	}
	if firstHash != secondHash {
		t.Fatal("the idempotency hash depends on the guest password")
	}
	if first.Guest.Password != "guest-password" {
		t.Fatal("hashing must not strip the password from the request")
	}
}

// A Turnstile token is single-use, so an idempotent retry carries a new one;
// the retry must still resolve to the original order.
func TestV2GuestRequestHashIgnoresTurnstileToken(t *testing.T) {
	ctx := context.Background()
	first := v2GuestPurchase("email", "guest@example.com")
	first.Guest.TurnstileToken = "first-token"
	second := v2GuestPurchase("email", "guest@example.com")
	second.Guest.TurnstileToken = "second-token"
	firstHash, err := requestHash(ctx, first)
	if err != nil {
		t.Fatalf("hash first request: %v", err)
	}
	secondHash, err := requestHash(ctx, second)
	if err != nil {
		t.Fatalf("hash second request: %v", err)
	}
	if firstHash != secondHash {
		t.Fatal("turnstile token changed the idempotency hash")
	}
	if first.Guest.TurnstileToken != "first-token" {
		t.Fatal("hashing must not strip the token from the request")
	}
	if other, _ := requestHash(ctx, v2GuestPurchase("email", "other@example.com")); other == firstHash {
		t.Fatal("a different guest identity must change the idempotency hash")
	}
}

func TestValidateV2CreateRequestRejectsWrongOrderTypeFields(t *testing.T) {
	for name, tt := range map[string]struct {
		req  *dto.V2CreateOrderRequest
		user *user.User
	}{
		"anonymous renewal":        {&dto.V2CreateOrderRequest{Type: v2OrderTypeRenewal, PaymentID: 2, UserSubscribeID: 8, Quantity: 1}, nil},
		"reset traffic quantity":   {&dto.V2CreateOrderRequest{Type: v2OrderTypeResetTraffic, PaymentID: 2, UserSubscribeID: 8, Quantity: 1}, &user.User{Id: 1}},
		"recharge with coupon":     {&dto.V2CreateOrderRequest{Type: v2OrderTypeRecharge, PaymentID: 2, Amount: 100, Coupon: "X"}, &user.User{Id: 1}},
		"user purchase with guest": {&dto.V2CreateOrderRequest{Type: v2OrderTypePurchase, PaymentID: 2, SubscribeID: 9, Quantity: 1, Guest: &dto.V2GuestOrderRequest{}}, &user.User{Id: 1}},
		"unknown type":             {&dto.V2CreateOrderRequest{Type: "gift", PaymentID: 2}, &user.User{Id: 1}},
		"missing payment":          {&dto.V2CreateOrderRequest{Type: v2OrderTypeRecharge, Amount: 100}, &user.User{Id: 1}},
	} {
		t.Run(name, func(t *testing.T) { assertCode(t, validateV2CreateRequest(tt.req, tt.user), xerr.InvalidParams) })
	}
}
