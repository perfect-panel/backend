package portal

import (
	"context"
	"fmt"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// guestShop is a plan on sale and an EPay method guests pay with.
type guestShop struct {
	*portalFixture
	plan   *subscribe.Subscribe
	method *payment.Payment
}

func newGuestShop(t *testing.T, adjust ...func(*Deps)) *guestShop {
	t.Helper()
	f := newPortalFixture(t, adjust...)
	return &guestShop{portalFixture: f, plan: f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 5 }), method: f.epay()}
}

func (s *guestShop) request(authType, identifier string) *dto.PortalPurchaseRequest {
	return &dto.PortalPurchaseRequest{
		AuthType: authType, Identifier: identifier, Password: "guest-password",
		Payment: s.method.Id, SubscribeId: s.plan.Id, Quantity: 1,
	}
}

// guestOrders lists the guest orders, which belong to no user yet.
func (s *guestShop) guestOrders() []*order.Order { return s.h.Orders(0) }

// The paid order inserts the guest auth method as given and issues a
// session, so an OAuth or device identifier would pre-bind someone else's
// identity.
func TestPortalPurchaseRejectsNonPasswordGuestAuthTypes(t *testing.T) {
	for _, authType := range []string{"github", "telegram", "google", "apple", "facebook", "device", ""} {
		t.Run(authType, func(t *testing.T) {
			s := newGuestShop(t)
			_, err := s.svc.Purchase(context.Background(), s.request(authType, "123456789"))
			assertCode(t, err, xerr.InvalidParams)
			if len(s.guestOrders()) != 0 {
				t.Fatal("a rejected identity created an order")
			}
		})
	}
}

func TestPortalPurchaseRejectsMalformedGuestIdentifiers(t *testing.T) {
	for _, tt := range []struct{ authType, identifier string }{
		{"email", "not-an-email"},
		{"email", "Alice <alice@example.com>"},
		{"email", ""},
		{"mobile", "12345"},
		{"mobile", "not-a-number"},
		{"mobile", ""},
	} {
		t.Run(tt.authType+"/"+tt.identifier, func(t *testing.T) {
			s := newGuestShop(t)
			_, err := s.svc.Purchase(context.Background(), s.request(tt.authType, tt.identifier))
			assertCode(t, err, xerr.InvalidParams)
			if len(s.guestOrders()) != 0 {
				t.Fatal("a malformed identity created an order")
			}
		})
	}
}

// The order stores the canonical identity the account flows store, so
// variants of one address cannot create a second account.
func TestPortalPurchaseCanonicalizesGuestIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, authType, identifier string
		wantType, wantIdentifier   string
	}{
		{"email", "email", "  Alice@Example.COM ", "email", "alice@example.com"},
		{"auth type case", " Email ", "alice@example.com", "email", "alice@example.com"},
		{"mobile with spacing", "mobile", "+86 155 0250 5555", "mobile", "+8615502505555"},
		{"mobile without plus", "mobile", "8615502505555", "mobile", "+8615502505555"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newGuestShop(t)
			if _, err := s.svc.Purchase(context.Background(), s.request(tt.authType, tt.identifier)); err != nil {
				t.Fatalf("Purchase: %v", err)
			}
			orders := s.guestOrders()
			if len(orders) != 1 || orders[0].GuestAuthType != tt.wantType || orders[0].GuestIdentifier != tt.wantIdentifier {
				t.Fatalf("orders = %+v, want one for (%s, %s)", orders, tt.wantType, tt.wantIdentifier)
			}
		})
	}
}

// A mailbox that already has an account is refused under its exact spelling
// and, as registration refuses it, under the other spellings that reach the
// same inbox: a paid order would otherwise open a second account for it.
func TestPortalPurchaseRefusesAnExistingAccount(t *testing.T) {
	for name, spelling := range map[string]string{
		"canonical form": "  Alice@Gmail.COM ",
		"gmail dots":     "a.li.ce@gmail.com",
		"plus tag":       "alice+shop@gmail.com",
	} {
		t.Run(name, func(t *testing.T) {
			s := newGuestShop(t)
			owner := s.h.User()
			if err := s.h.DB.Create(&user.AuthMethods{UserId: owner.Id, AuthType: "email", AuthIdentifier: "alice@gmail.com"}).Error; err != nil {
				t.Fatal(err)
			}
			_, err := s.svc.Purchase(context.Background(), s.request("email", spelling))
			assertCode(t, err, xerr.UserExist)
			if len(s.guestOrders()) != 0 {
				t.Fatal("an order was created for an existing account")
			}
		})
	}
}

// Guest purchases open an account once paid, so the registration policy
// applies to them: registration may be stopped, the sign-in method disabled
// or the email domain outside the allowlist. Without a policy nothing is
// gated.
func TestPortalPurchaseAppliesTheRegistrationPolicy(t *testing.T) {
	open := RegistrationPolicy{EmailEnabled: true, MobileEnabled: true}
	refused := []struct {
		name                 string
		policy               RegistrationPolicy
		authType, identifier string
		code                 uint32
	}{
		{"registration stopped", RegistrationPolicy{StopRegister: true, EmailEnabled: true}, "email", "guest@example.com", xerr.StopRegister},
		{"email sign-in disabled", RegistrationPolicy{MobileEnabled: true}, "email", "guest@example.com", xerr.GetAuthenticatorError},
		{"mobile sign-in disabled", RegistrationPolicy{EmailEnabled: true}, "mobile", "+8615502505555", xerr.GetAuthenticatorError},
		{"domain outside the allowlist", RegistrationPolicy{EmailEnabled: true, EmailEnableDomainSuffix: true, EmailDomainSuffixList: "example.org"}, "email", "guest@example.com", xerr.InvalidParams},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			policy := tt.policy
			s := newGuestShop(t, func(d *Deps) { d.Config.Registration = func() RegistrationPolicy { return policy } })
			_, err := s.svc.Purchase(context.Background(), s.request(tt.authType, tt.identifier))
			assertCode(t, err, tt.code)
			if len(s.guestOrders()) != 0 {
				t.Fatal("a gated guest purchase created an order")
			}
		})
	}
	t.Run("allowlisted domain", func(t *testing.T) {
		policy := RegistrationPolicy{EmailEnabled: true, EmailEnableDomainSuffix: true, EmailDomainSuffixList: "example.org, example.com"}
		s := newGuestShop(t, func(d *Deps) { d.Config.Registration = func() RegistrationPolicy { return policy } })
		if _, err := s.svc.Purchase(context.Background(), s.request("email", "guest@mail.example.com")); err != nil {
			t.Fatalf("Purchase: %v", err)
		}
	})
	t.Run("open registration", func(t *testing.T) {
		s := newGuestShop(t, func(d *Deps) { d.Config.Registration = func() RegistrationPolicy { return open } })
		if _, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com")); err != nil {
			t.Fatalf("Purchase: %v", err)
		}
	})
	t.Run("no policy", func(t *testing.T) {
		s := newGuestShop(t)
		if _, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com")); err != nil {
			t.Fatalf("Purchase: %v", err)
		}
	})
}

func TestPortalPurchaseVerifiesTurnstileWhenEnabled(t *testing.T) {
	type call struct{ secret, token, ip string }
	for _, tt := range []struct {
		name      string
		policy    GuestVerification
		token     string
		verified  bool
		wantErr   bool
		wantCalls int
	}{
		{name: "disabled skips the challenge", policy: GuestVerification{Enabled: false, Secret: "secret"}},
		{name: "missing token", policy: GuestVerification{Enabled: true, Secret: "secret"}, wantErr: true},
		{name: "missing secret fails closed", policy: GuestVerification{Enabled: true}, token: "token", wantErr: true},
		{name: "rejected token", policy: GuestVerification{Enabled: true, Secret: "secret"}, token: "token", wantErr: true, wantCalls: 1},
		{name: "verified token", policy: GuestVerification{Enabled: true, Secret: "secret"}, token: "token", verified: true, wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []call
			s := newGuestShop(t, func(d *Deps) {
				policy := tt.policy
				d.Config.GuestVerification = func() GuestVerification { return policy }
				d.VerifyTurnstile = func(_ context.Context, secret, token, ip string) (bool, error) {
					calls = append(calls, call{secret, token, ip})
					return tt.verified, nil
				}
			})
			ctx := requestmeta.With(context.Background(), requestmeta.New("203.0.113.7", "test-agent"))
			req := s.request("email", "guest@example.com")
			req.TurnstileToken = tt.token

			_, err := s.svc.Purchase(ctx, req)
			if tt.wantErr {
				assertCode(t, err, xerr.TooManyRequests)
				if len(s.guestOrders()) != 0 {
					t.Fatal("an unverified guest purchase created an order")
				}
			} else if err != nil {
				t.Fatalf("Purchase: %v", err)
			}
			if len(calls) != tt.wantCalls {
				t.Fatalf("verifier calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if len(calls) == 1 && calls[0] != (call{"secret", "token", "203.0.113.7"}) {
				t.Fatalf("verifier call = %+v", calls[0])
			}
		})
	}
}

func TestPortalPurchaseCapsPendingGuestOrders(t *testing.T) {
	s := newGuestShop(t)
	for i := range maxPendingGuestOrders - 1 {
		s.h.Order(&order.Order{OrderNo: fmt.Sprintf("pending-%d", i), Status: order.StatusPending, GuestAuthType: "email", GuestIdentifier: "guest@example.com"})
	}
	if _, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com")); err != nil {
		t.Fatalf("Purchase below the cap: %v", err)
	}
	_, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com"))
	assertCode(t, err, xerr.TooManyRequests)
	if got := len(s.guestOrders()); got != maxPendingGuestOrders {
		t.Fatalf("guest orders = %d, want %d", got, maxPendingGuestOrders)
	}
}

// A pending order past the unpaid close age is abandoned: a gateway without
// a query API never confirms it closed, so it stays pending for good and
// must not lock the identity out. Only younger pending orders count.
func TestPortalPurchaseCapIgnoresAbandonedPendingOrders(t *testing.T) {
	s := newGuestShop(t)
	abandoned := time.Now().Add(-order.UnpaidCloseAge - time.Minute)
	for i := range maxPendingGuestOrders {
		s.h.Order(&order.Order{OrderNo: fmt.Sprintf("abandoned-%d", i), Status: order.StatusPending, GuestAuthType: "email", GuestIdentifier: "guest@example.com", CreatedAt: abandoned})
	}
	if _, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com")); err != nil {
		t.Fatalf("Purchase with only abandoned pending orders: %v", err)
	}
	for i := range maxPendingGuestOrders - 1 {
		s.h.Order(&order.Order{OrderNo: fmt.Sprintf("recent-%d", i), Status: order.StatusPending, GuestAuthType: "email", GuestIdentifier: "guest@example.com"})
	}
	_, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com"))
	assertCode(t, err, xerr.TooManyRequests)
}

// A guest order holds a plan unit and returns a capability the buyer pays
// with; its expiry close is scheduled.
func TestPortalPurchaseReservesTheOrder(t *testing.T) {
	s := newGuestShop(t)
	resp, err := s.svc.Purchase(context.Background(), s.request("email", "guest@example.com"))
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	created := s.h.ReloadOrder(resp.OrderNo)
	if created.GuestCheckoutTokenHash != order.CheckoutTokenHash(resp.CheckoutToken) || created.GuestPasswordHash == "" || created.GuestPasswordHash == "guest-password" {
		t.Fatalf("order = %+v, want the capability hash and a hashed password", created)
	}
	if s.h.ReloadPlan(s.plan.Id).Inventory != 4 || len(s.queue.DeferredCloses) != 1 || s.queue.DeferredCloses[0] != resp.OrderNo {
		t.Fatal("the guest order did not reserve stock or schedule its close")
	}
	if _, err := s.checkout(context.Background(), resp.OrderNo, resp.CheckoutToken); err != nil {
		t.Fatalf("checkout with the returned capability: %v", err)
	}
}

// A coupon's per-user limit binds a guest by the identity the order names:
// its pending orders hold a reservation each and its settled orders consumed
// a use, before and after the account exists. Without it one identity could
// reserve a once-per-user coupon on each of its pending orders.
func TestPortalPurchaseAppliesTheCouponUserLimitToTheGuestIdentity(t *testing.T) {
	s := newGuestShop(t)
	s.h.Coupon("ONCE", func(c *coupon.Coupon) { c.Count = 5; c.UserLimit = 1 })
	withCoupon := func(identifier string) *dto.PortalPurchaseRequest {
		req := s.request("email", identifier)
		req.Coupon = "ONCE"
		return req
	}

	first, err := s.svc.Purchase(context.Background(), withCoupon("guest@example.com"))
	if err != nil {
		t.Fatalf("first Purchase: %v", err)
	}
	_, err = s.svc.Purchase(context.Background(), withCoupon("guest@example.com"))
	assertCode(t, err, xerr.CouponInsufficientUsage)
	if used := s.h.ReloadCoupon("ONCE").UsedCount; used != 1 {
		t.Fatalf("coupon uses = %d, want the one reservation", used)
	}
	if _, err := s.svc.Purchase(context.Background(), withCoupon("other@example.com")); err != nil {
		t.Fatalf("another identity: %v", err)
	}
	// The activated order is bound to its account and keeps its identity.
	if err := s.h.DB.Model(&order.Order{}).Where("order_no = ?", first.OrderNo).Updates(map[string]any{"user_id": 42, "status": order.StatusFinished}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.svc.Purchase(context.Background(), withCoupon("guest@example.com"))
	assertCode(t, err, xerr.CouponInsufficientUsage)
	// A closed order returned its reservation and no longer counts.
	if err := s.h.DB.Model(&order.Order{}).Where("order_no = ?", first.OrderNo).Update("status", order.StatusClosed).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.svc.Purchase(context.Background(), withCoupon("guest@example.com")); err != nil {
		t.Fatalf("after the first order closed: %v", err)
	}
}

// uncountingOrders is an order store without the guest coupon counter.
type uncountingOrders struct{ Orders }

// Without a counter the per-user limit cannot be checked for a guest; a
// limited coupon is refused rather than granted without limit, while an
// unlimited one is unaffected.
func TestPortalPurchaseRefusesALimitedCouponItCannotCount(t *testing.T) {
	s := newGuestShop(t, func(d *Deps) { d.Orders = uncountingOrders{Orders: d.Orders} })
	s.h.Coupon("ONCE", func(c *coupon.Coupon) { c.Count = 5; c.UserLimit = 1 })
	s.h.Coupon("FREE", func(c *coupon.Coupon) { c.Count = 5 })
	req := s.request("email", "guest@example.com")
	req.Coupon = "ONCE"
	_, err := s.svc.Purchase(context.Background(), req)
	assertCode(t, err, xerr.CouponNotApplicable)
	if s.h.ReloadCoupon("ONCE").UsedCount != 0 || len(s.guestOrders()) != 0 {
		t.Fatal("an uncountable limited coupon was reserved")
	}
	req.Coupon = "FREE"
	if _, err := s.svc.Purchase(context.Background(), req); err != nil {
		t.Fatalf("unlimited coupon: %v", err)
	}
}

// Guests have no wallet.
func TestPortalPurchaseRejectsBalancePayment(t *testing.T) {
	s := newGuestShop(t)
	req := s.request("email", "guest@example.com")
	req.Payment = s.balance().Id
	_, err := s.svc.Purchase(context.Background(), req)
	assertCode(t, err, xerr.PaymentMethodNotFound)
}

// soldOut is the inventory of a plan another buyer just emptied.
type soldOut struct{}

func (soldOut) Reserve(context.Context, string, int64) error { return subscription.ErrOutOfStock }

// A guest order whose plan sold out meanwhile is closed again and returns
// its coupon use.
func TestPortalPurchaseOfASoldOutPlanReleasesTheOrder(t *testing.T) {
	s := newGuestShop(t, func(d *Deps) { d.Inventory = soldOut{} })
	s.h.Coupon("SAVE", func(c *coupon.Coupon) { c.Count = 5 })
	req := s.request("email", "guest@example.com")
	req.Coupon = "SAVE"

	_, err := s.svc.Purchase(context.Background(), req)
	assertCode(t, err, xerr.SubscribeOutOfStock)
	orders := s.guestOrders()
	if len(orders) != 1 || orders[0].Status != order.StatusClosed || s.h.ReloadCoupon("SAVE").UsedCount != 0 {
		t.Fatalf("orders = %+v, want the order closed and its coupon use returned", orders)
	}
}

// A guest pays exactly what the preview showed.
func TestPortalPreviewEqualsTheCreatedOrder(t *testing.T) {
	for _, tt := range []struct {
		name     string
		quantity int64
		tiers    string
		coupon   *coupon.Coupon
		fee      func(*payment.Payment)
		want     pricing.Quote
	}{
		{name: "list price", quantity: 1, want: pricing.Quote{Price: 1000, Amount: 1000}},
		{name: "tier and percent coupon", quantity: 3, tiers: `[{"quantity":3,"discount":90}]`,
			coupon: &coupon.Coupon{Type: coupon.TypePercentage, Discount: 10},
			want:   pricing.Quote{Price: 3000, Discount: 300, CouponDiscount: 270, Amount: 2430}},
		{name: "fixed coupon and fee", quantity: 1, coupon: &coupon.Coupon{Type: coupon.TypeFixed, Discount: 150},
			fee:  func(p *payment.Payment) { p.FeeMode, p.FeePercent, p.FeeAmount = 3, 5, 20 },
			want: pricing.Quote{Price: 1000, CouponDiscount: 150, FeeAmount: 62, Amount: 912}}, // 5% of 850 is 42.5, floored
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newPortalFixture(t)
			plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Discount = tt.tiers })
			var adjust []func(*payment.Payment)
			if tt.fee != nil {
				adjust = append(adjust, tt.fee)
			}
			method := f.h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`, adjust...)
			code := ""
			if tt.coupon != nil {
				code = "GUEST"
				f.h.Coupon(code, func(c *coupon.Coupon) { c.Type, c.Discount = tt.coupon.Type, tt.coupon.Discount })
			}

			preview, err := f.svc.PrePurchase(context.Background(), &dto.PrePurchaseOrderRequest{SubscribeId: plan.Id, Quantity: tt.quantity, Payment: method.Id, Coupon: code})
			if err != nil {
				t.Fatalf("PrePurchase: %v", err)
			}
			got := pricing.Quote{Price: preview.Price, Discount: preview.Discount, CouponDiscount: preview.CouponDiscount, FeeAmount: preview.FeeAmount, Amount: preview.Amount}
			if got != tt.want || preview.Coupon != code {
				t.Fatalf("preview = %+v, want %+v with coupon %q", preview, tt.want, code)
			}
			resp, err := f.svc.Purchase(context.Background(), &dto.PortalPurchaseRequest{
				AuthType: "email", Identifier: "guest@example.com", Password: "guest-password",
				SubscribeId: plan.Id, Quantity: tt.quantity, Payment: method.Id, Coupon: code,
			})
			if err != nil {
				t.Fatalf("Purchase: %v", err)
			}
			o := f.h.ReloadOrder(resp.OrderNo)
			created := pricing.Quote{Price: o.Price, Discount: o.Discount, CouponDiscount: o.CouponDiscount, GiftAmount: o.GiftAmount, FeeAmount: o.FeeAmount, Amount: o.Amount}
			if created != tt.want || o.Coupon != code {
				t.Fatalf("order = %+v, want %+v with coupon %q", created, tt.want, code)
			}
		})
	}
}
