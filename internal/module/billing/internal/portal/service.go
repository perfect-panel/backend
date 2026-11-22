// Package portal implements the guest storefront subdomain of the billing
// module: guest pre-orders, gateway/balance checkout, order status polling
// with session exchange, and the public plan/payment listings. Only the
// module facade may reach it.
package portal

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// GuestSessionExchangeWindow bounds how long after its order was settled a
// guest checkout capability may still be exchanged for a session. The
// capability lives in the buyer's browser and in the order row for the
// order's lifetime, unlike a session, which a password change or reset
// revokes; the buyer polls the order right after paying, so an hour covers a
// slow activation and a page reload while denying anyone who obtains the
// capability later a way into the account.
const GuestSessionExchangeWindow = time.Hour

// settlementEventScan is how many of an order's events the settlement time
// is looked for in; an order has a handful.
const settlementEventScan = 100

// PlanReader is the subdomain's port onto the subscription domain's plan
// catalogue; the legacy subscribe repository satisfies it structurally.
type PlanReader interface {
	FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error)
	FilterList(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error)
}

// GuestAccountReader is the subdomain's port onto the identity domain: guest
// purchase must refuse identifiers that already have an account.
type GuestAccountReader interface {
	FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error)
}

// EmailAliasReader is the extension of GuestAccountReader the mailbox-alias
// check needs: the binding of a live account that reaches the same mailbox
// as an email under another spelling (Gmail dots, "+tag" subaddresses), as
// registration checks it. The identity repository has it; a reader without
// it cannot check aliases, and the flows say so in their reports.
type EmailAliasReader interface {
	FindEmailAlias(ctx context.Context, email string) (*user.AuthMethods, error)
}

// SessionStore issues the Redis-backed session created after a guest
// purchase completes; the redis client satisfies it structurally.
type SessionStore interface {
	// The session epochs (usersession.Store) live next to the sessions.
	usersession.Store
}

// GuestCheckoutCache provides the one Redis operation needed to validate
// legacy guest checkout capabilities.
type GuestCheckoutCache interface {
	Get(ctx context.Context, key string) *redis.StringCmd
}

// ExchangeRateCache is shared with the rate refresh task. It is deliberately
// limited to the checkout use case's read/write needs.
type ExchangeRateCache interface {
	Get() float64
	Set(float64)
}

// Orders is the order persistence the storefront flows use outside a
// transaction.
type Orders interface {
	FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error)
	// CountPendingGuestOrders counts the unpaid orders a canonical guest
	// identity created at or after since and still holds before its account
	// exists.
	CountPendingGuestOrders(ctx context.Context, authType, identifier string, since time.Time) (int64, error)
	UpdatePaymentExpectation(ctx context.Context, orderNo string, amount int64, currency string) (bool, error)
	SetPaymentTradeNoIfEmpty(ctx context.Context, orderNo, tradeNo string) (bool, error)
	BeginPaymentSession(ctx context.Context, orderNo string) (bool, error)
	CompletePaymentSession(ctx context.Context, orderNo, tradeNo, session string) (bool, error)
	UpdateOrderStatusFrom(ctx context.Context, orderNo string, from, status uint8) (bool, error)
}

// OrderEvents reads an order's durable events; the payment event dates the
// order's settlement.
type OrderEvents interface {
	ListAfter(ctx context.Context, orderNo string, afterID int64, limit int) ([]*order.Event, error)
}

// PaymentMethods loads and lists payment methods.
type PaymentMethods interface {
	FindOne(ctx context.Context, id int64) (*payment.Payment, error)
	FindAvailableMethods(ctx context.Context) ([]*payment.Payment, error)
}

// Transactor runs billing-scoped transactions.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// UserCache drops a user's cached projection after a wallet movement; the
// identity module provides it.
type UserCache interface {
	ClearUserCache(ctx context.Context, userIDs ...int64) error
}

// OrderQueue mirrors the facade's order queue port: the deferred close of a
// new guest order and the activation of a balance-paid order.
type OrderQueue interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
	EnqueueDeferredClose(ctx context.Context, orderNo string) error
}

type Inventory interface {
	Reserve(context.Context, string, int64) error
}

// Config is the static configuration snapshot for the portal flows. ClientIP
// is deliberately absent: it is resolved per request from the context.
type Config struct {
	// SiteName/CurrencyUnit/CurrencyAccessKey/SiteHost/GuestVerification are
	// runtime-mutable (the admin edits them and ReinitSubsystem reloads);
	// read per request.
	SiteName          func() string
	CurrencyUnit      func() string
	CurrencyAccessKey func() string
	// SiteHost is the configured public site host, the base of payment notify
	// URLs when a method has no Domain.
	SiteHost func() string
	// GuestVerification is the Turnstile policy for guest purchases, which
	// create an account once paid and therefore follow the registration check.
	GuestVerification func() GuestVerification
	// Registration is the registration policy guest purchases follow for the
	// same reason; nil applies no policy.
	Registration func() RegistrationPolicy
	JwtSecret    string
	JwtExpire    int64
}

// GuestVerification is the per-request Turnstile policy for guest purchases.
type GuestVerification struct {
	Enabled bool
	Secret  string
}

// RegistrationPolicy is the per-request snapshot of the registration
// settings a new account is subject to, as identity's registration applies
// them: registration may be stopped, the account's sign-in method may be
// disabled, and an email address may have to belong to an allowlisted
// domain.
type RegistrationPolicy struct {
	StopRegister  bool
	EmailEnabled  bool
	MobileEnabled bool
	// EmailDomainSuffixList is the domain allowlist, applied when
	// EmailEnableDomainSuffix is set.
	EmailDomainSuffixList   string
	EmailEnableDomainSuffix bool
}

// TurnstileVerifier checks a Cloudflare Turnstile response token.
type TurnstileVerifier func(ctx context.Context, secret, token, remoteIP string) (bool, error)

type Deps struct {
	Orders      Orders
	OrderEvents OrderEvents
	Coupons     pricing.CouponFinder
	Payments    PaymentMethods
	UserAuths   GuestAccountReader
	Plans       PlanReader
	Tx          Transactor
	UserCache   UserCache
	// Inventory changes belong to the separately injected capability.
	Inventory          Inventory
	Sessions           SessionStore
	Queue              OrderQueue
	GuestCheckoutCache GuestCheckoutCache
	ExchangeRate       ExchangeRateCache
	// Gateways opens the gateway of an order's payment method; nil selects
	// the production gateways.
	Gateways *gateway.Registry
	Config   Config
	// VerifyTurnstile overrides the Cloudflare siteverify client; nil selects
	// the default.
	VerifyTurnstile TurnstileVerifier
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	if deps.Gateways == nil {
		deps.Gateways = gateway.NewRegistry()
	}
	return &Service{deps: deps}
}

func (s *Service) siteName() string       { return snapshot(s.deps.Config.SiteName) }
func (s *Service) currencyUnit() string   { return snapshot(s.deps.Config.CurrencyUnit) }
func (s *Service) currencyAccess() string { return snapshot(s.deps.Config.CurrencyAccessKey) }
func (s *Service) siteHost() string       { return snapshot(s.deps.Config.SiteHost) }

// snapshot reads an optional runtime-mutable setting.
func snapshot(read func() string) string {
	if read == nil {
		return ""
	}
	return read()
}

// IssueSession creates the normal authenticated session issued after a guest
// purchase completes.  Both V1's status endpoint and V2's explicit
// capability-exchange endpoint use this helper so their token and Redis
// session semantics cannot drift.
func (s *Service) IssueSession(ctx context.Context, userID int64) (string, error) {
	// The same session every sign-in issues: it carries the user's epoch, so a
	// password change or reset ends it too.
	token, err := usersession.Issue(ctx, s.deps.Sessions, s.deps.Config.JwtSecret, s.deps.Config.JwtExpire, usersession.Grant{UserID: userID})
	if err != nil {
		return "", xerr.Wrapf(err, xerr.ERROR, "issue session: %v", err)
	}
	return token, nil
}

// ExchangeGuestSession is the one rule under which a guest checkout
// capability becomes a session, shared by V1's status endpoint and V2's
// session endpoint: the order's account must exist, the order must have
// been settled within GuestSessionExchangeWindow, and the account's sessions
// must not have been revoked since the settlement, because a revocation
// (password change or reset, an administrator's edit) means the capability
// holder is no longer trusted with the account. A refusal is InvalidAccess.
func (s *Service) ExchangeGuestSession(ctx context.Context, orderInfo *order.Order) (string, error) {
	if orderInfo.UserId == 0 || !order.IsSettled(orderInfo.Status) {
		return "", xerr.Errorf(xerr.OrderStatusError, "guest account is not ready")
	}
	settledAt, err := s.settledAt(ctx, orderInfo)
	if err != nil {
		return "", err
	}
	if settledAt.IsZero() || timeutil.Now().Sub(settledAt) > GuestSessionExchangeWindow {
		return "", xerr.Errorf(xerr.InvalidAccess, "guest checkout token can no longer be exchanged for a session")
	}
	revoked, err := usersession.RevokedSince(ctx, s.deps.Sessions, orderInfo.UserId, settledAt)
	if err != nil {
		return "", xerr.Wrapf(err, xerr.ERROR, "check the sessions of user %d", orderInfo.UserId)
	}
	if revoked {
		return "", xerr.Errorf(xerr.InvalidAccess, "the account's sessions were revoked after the order was settled")
	}
	return s.IssueSession(ctx, orderInfo.UserId)
}

// settledAt is when the order's payment was collected: the time of its
// payment event. The zero time reports an order without one, settled before
// events were recorded or whose events were pruned; both are far older than
// the exchange window.
func (s *Service) settledAt(ctx context.Context, orderInfo *order.Order) (time.Time, error) {
	if s.deps.OrderEvents == nil {
		return time.Time{}, nil
	}
	events, err := s.deps.OrderEvents.ListAfter(ctx, orderInfo.OrderNo, 0, settlementEventScan)
	if err != nil {
		return time.Time{}, xerr.Wrapf(err, xerr.DatabaseQueryError, "list events of order %s", orderInfo.OrderNo)
	}
	for _, event := range events {
		if event.EventType == order.EventTypePaymentPaid {
			return event.CreatedAt, nil
		}
	}
	return time.Time{}, nil
}

// findOrder loads an order a request names: a missing order is
// OrderNotExist, a failed lookup a database error.
func (s *Service) findOrder(ctx context.Context, orderNo string) (*order.Order, error) {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Errorf(xerr.OrderNotExist, "order not exist: %v", orderNo)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find order %s", orderNo)
	}
	return orderInfo, nil
}
