package order

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type v2TicketOrderRepo struct {
	repository.OrderRepo
	order *orderEntity.Order
}

func (r *v2TicketOrderRepo) FindOneByOrderNo(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if r.order == nil || r.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.order
	return &copy, nil
}

type v2TicketStore struct {
	repository.Store
	orders repository.OrderRepo
}

func (s v2TicketStore) Order() repository.OrderRepo { return s.orders }

func TestV2OrderRequestHashIgnoresReturnURLAndBindsUser(t *testing.T) {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 17})
	logic := NewV2OrderLogic(ctx, &svc.ServiceContext{})
	first := &dto.V2CreateOrderRequest{
		Type: v2OrderTypePurchase, PaymentID: 3, SubscribeID: 9, Quantity: 2, Coupon: "SUMMER",
		ReturnURL: "https://one.example/result",
	}
	second := *first
	second.ReturnURL = "https://two.example/result"
	firstHash, err := logic.requestHash(first)
	if err != nil {
		t.Fatalf("hash first request: %v", err)
	}
	secondHash, err := logic.requestHash(&second)
	if err != nil {
		t.Fatalf("hash second request: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("return_url changed stable hash: %s != %s", firstHash, secondHash)
	}
	second.Coupon = "OTHER"
	thirdHash, err := logic.requestHash(&second)
	if err != nil {
		t.Fatalf("hash changed request: %v", err)
	}
	if firstHash == thirdHash {
		t.Fatal("business request change must change idempotency hash")
	}
}

func TestV2GuestCheckoutTokenIsDeterministicPerIdempotencyKey(t *testing.T) {
	logic := NewV2OrderLogic(context.Background(), &svc.ServiceContext{Config: config.Config{JwtAuth: config.JwtAuth{AccessSecret: "stream-secret"}}})
	first := logic.derivedGuestCheckoutToken("1234567890abcdef")
	second := logic.derivedGuestCheckoutToken("1234567890abcdef")
	third := logic.derivedGuestCheckoutToken("abcdef1234567890")
	if first == "" || first != second || first == third {
		t.Fatalf("derived guest capability is not deterministic and key-bound")
	}
}

func TestV2OrderEventTicketBindsCurrentOrderOwner(t *testing.T) {
	orderInfo := &orderEntity.Order{
		OrderNo: "order-ticket", UserId: 17, Status: 1, CreatedAt: time.Now(), StateVersion: 1,
	}
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 17})
	logic := NewV2OrderLogic(ctx, &svc.ServiceContext{
		Config: config.Config{JwtAuth: config.JwtAuth{AccessSecret: "stream-secret"}},
		Store:  v2TicketStore{orders: &v2TicketOrderRepo{order: orderInfo}},
	})
	ticket, _, err := logic.mintEventTicket(orderInfo, "")
	if err != nil {
		t.Fatalf("mint ticket: %v", err)
	}
	claimed, err := logic.AuthorizeEventTicket(orderInfo.OrderNo, ticket)
	if err != nil {
		t.Fatalf("authorize ticket: %v", err)
	}
	if claimed.OrderNo != orderInfo.OrderNo {
		t.Fatalf("claimed order = %q, want %q", claimed.OrderNo, orderInfo.OrderNo)
	}
	if _, err := logic.AuthorizeEventTicket("other-order", ticket); err == nil {
		t.Fatal("ticket must not authorize a different order")
	}
	expired, err := jwt.NewJwtToken("stream-secret", time.Now().Add(-time.Minute).Unix(), 1,
		jwt.WithOption("OrderNo", orderInfo.OrderNo), jwt.WithOption("Scope", v2EventScope), jwt.WithOption("UserId", orderInfo.UserId))
	if err != nil {
		t.Fatalf("mint expired ticket: %v", err)
	}
	if _, err := logic.EventTicketExpiresAt(expired); err == nil {
		t.Fatal("expired stream ticket must be rejected")
	}
}

func TestV2GuestCapabilitySurvivesAccountActivation(t *testing.T) {
	const (
		secret          = "stream-secret"
		idempotencyKey  = "1234567890abcdef"
		guestCapability = "guest-checkout-capability"
	)
	orderInfo := &orderEntity.Order{
		OrderNo: "guest-order", Status: 2, CreatedAt: time.Now(), StateVersion: 2,
		GuestAuthType: "email", GuestIdentifier: "guest@example.com",
		GuestCheckoutTokenHash: constant.CheckoutTokenHash(guestCapability),
	}
	orders := &v2TicketOrderRepo{order: orderInfo}
	ctx := context.Background()
	logic := NewV2OrderLogic(ctx, &svc.ServiceContext{
		Config: config.Config{JwtAuth: config.JwtAuth{AccessSecret: secret}},
		Store:  v2TicketStore{orders: orders},
	})

	ticket, _, err := logic.mintEventTicket(orderInfo, guestCapability)
	if err != nil {
		t.Fatalf("mint guest ticket: %v", err)
	}
	// Activation creates the user after payment, while the browser may have an
	// already-issued stream ticket and a persisted checkout capability.
	orders.order.UserId = 42
	orders.order.Status = 5

	if _, err := logic.AuthorizeEventTicket(orderInfo.OrderNo, ticket); err != nil {
		t.Fatalf("pre-activation guest ticket must reconnect after account creation: %v", err)
	}
	if _, err := logic.EventTicket(orderInfo.OrderNo, guestCapability); err != nil {
		t.Fatalf("guest capability must refresh ticket after account creation: %v", err)
	}
	orders.order.GuestCheckoutTokenHash = constant.CheckoutTokenHash("replaced-capability")
	if _, err := logic.AuthorizeEventTicket(orderInfo.OrderNo, ticket); err == nil {
		t.Fatal("ticket must be rejected when its guest capability is no longer valid")
	}
	orders.order.GuestCheckoutTokenHash = constant.CheckoutTokenHash(guestCapability)
	if err := logic.authorizeExistingCreate(orders.order, &dto.V2CreateOrderRequest{
		Type:  v2OrderTypePurchase,
		Guest: &dto.V2GuestOrderRequest{AuthType: "email", Identifier: "guest@example.com"},
	}, guestCapability); err != nil {
		t.Fatalf("idempotent guest recovery must remain authorized after account creation: %v", err)
	}
	if got := checkoutTokenForResponse(orders.order, guestCapability); got != guestCapability {
		t.Fatalf("checkout capability = %q, want it retained for recovery", got)
	}
}

func TestV2GuestSessionExchangeRequiresActivatedAccount(t *testing.T) {
	const guestCapability = "guest-checkout-capability"
	orderInfo := &orderEntity.Order{
		OrderNo: "guest-session", Status: 2, CreatedAt: time.Now(), StateVersion: 2,
		GuestCheckoutTokenHash: constant.CheckoutTokenHash(guestCapability),
	}
	orders := &v2TicketOrderRepo{order: orderInfo}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	logic := NewV2OrderLogic(context.Background(), &svc.ServiceContext{
		Config: config.Config{JwtAuth: config.JwtAuth{AccessSecret: "session-secret", AccessExpire: 3600}},
		Store:  v2TicketStore{orders: orders},
		Redis:  redisClient,
	})

	if _, err := logic.Session(orderInfo.OrderNo, guestCapability); err == nil {
		t.Fatal("session exchange must wait for guest account creation")
	}
	orders.order.UserId = 42
	response, err := logic.Session(orderInfo.OrderNo, guestCapability)
	if err != nil {
		t.Fatalf("exchange guest capability for session: %v", err)
	}
	claims, err := jwt.ParseJwtToken(response.AccessToken, "session-secret")
	if err != nil || claimInt64(claims, "UserId") != 42 {
		t.Fatalf("session claims = %#v, %v; want user 42", claims, err)
	}
	sessionID, _ := claims["SessionId"].(string)
	storedUserID, err := redisClient.Get(context.Background(), fmt.Sprintf("%v:%v", config.SessionIdKey, sessionID)).Result()
	if err != nil || storedUserID != "42" {
		t.Fatalf("session cache = (%q, %v), want user 42", storedUserID, err)
	}
	if _, err := logic.Session(orderInfo.OrderNo, "incorrect-capability"); err == nil {
		t.Fatal("invalid checkout capability must not issue a session")
	}
}

func TestValidateV2CreateRequestRejectsWrongOrderTypeFields(t *testing.T) {
	err := validateV2CreateRequest(&dto.V2CreateOrderRequest{
		Type: v2OrderTypeRenewal, PaymentID: 2, UserSubscribeID: 8, Quantity: 1,
	}, nil)
	if err == nil {
		t.Fatal("anonymous renewal must be rejected")
	}
	err = validateV2CreateRequest(&dto.V2CreateOrderRequest{
		Type: v2OrderTypeResetTraffic, PaymentID: 2, UserSubscribeID: 8, Quantity: 1,
	}, &userEntity.User{Id: 1})
	if err == nil {
		t.Fatal("reset traffic quantity must be rejected")
	}
}
