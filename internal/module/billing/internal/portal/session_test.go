package portal

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The guest order the session exchange tests settle and its capability.
const (
	settledOrderNo    = "guest-1"
	settledCapability = "capability"
)

// settledGuestOrder seeds a guest order of a plan as the payment callback and
// the activation leave it: paid now, with its payment event, bound to the
// account the activation created.
func (f *portalFixture) settledGuestOrder() (*user.User, *order.Order) {
	f.t.Helper()
	u := f.h.User()
	plan := f.h.Plan(1000)
	o := f.pendingOrder(settledOrderNo, 0, 1000, f.epay(), guestToken(settledCapability), func(o *order.Order) { o.SubscribeId = plan.Id })
	paid, err := f.h.Store.Order().MarkOrderPaid(context.Background(), o.OrderNo, "trade-"+settledOrderNo)
	if err != nil || !paid {
		f.t.Fatalf("MarkOrderPaid = (%t, %v)", paid, err)
	}
	if err := f.h.DB.Model(&order.Order{}).Where("order_no = ?", settledOrderNo).Update("user_id", u.Id).Error; err != nil {
		f.t.Fatal(err)
	}
	return u, f.h.ReloadOrder(settledOrderNo)
}

// settledAt moves the order's payment event to at. The column is
// create-only for the application, so the row is changed directly.
func (f *portalFixture) settledAt(orderNo string, at time.Time) {
	f.t.Helper()
	result := f.h.DB.Exec("UPDATE order_event SET created_at = ? WHERE order_no = ? AND event_type = ?", at, orderNo, order.EventTypePaymentPaid)
	if result.Error != nil || result.RowsAffected != 1 {
		f.t.Fatalf("move payment event: %d rows, %v", result.RowsAffected, result.Error)
	}
}

func (f *portalFixture) status(ctx context.Context, orderNo, token string) (*dto.QueryPurchaseOrderResponse, error) {
	return f.svc.QueryPurchaseOrder(ctx, &dto.QueryPurchaseOrderRequest{OrderNo: orderNo, CheckoutToken: token})
}

// assertSessionOf checks that token is a live session of u.
func (f *portalFixture) assertSessionOf(token string, u *user.User) {
	f.t.Helper()
	claims, err := usersession.Validate(context.Background(), f.h.Redis, "jwt-secret", token)
	if err != nil || claims.UserID != u.Id {
		f.t.Fatalf("session claims = %+v, %v; want a session of user %d", claims, err, u.Id)
	}
}

// Right after the payment the capability yields a session of the account the
// order created.
func TestGuestStatusExchangesTheCapabilityForASession(t *testing.T) {
	f := newPortalFixture(t)
	u, o := f.settledGuestOrder()

	resp, err := f.status(context.Background(), o.OrderNo, settledCapability)
	if err != nil || resp.Token == "" {
		t.Fatalf("QueryPurchaseOrder = (%+v, %v), want a session token", resp, err)
	}
	f.assertSessionOf(resp.Token, u)
}

// The capability stays in the browser and the order row for good; an hour
// after the settlement it no longer opens the account. An order without a
// payment event was settled long before events were recorded.
func TestGuestStatusRefusesTheCapabilityAfterTheExchangeWindow(t *testing.T) {
	f := newPortalFixture(t)
	_, o := f.settledGuestOrder()
	f.settledAt(o.OrderNo, time.Now().Add(-GuestSessionExchangeWindow-time.Minute))

	_, err := f.status(context.Background(), o.OrderNo, settledCapability)
	assertCode(t, err, xerr.InvalidAccess)

	if err := f.h.DB.Where("order_no = ?", o.OrderNo).Delete(&order.Event{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = f.status(context.Background(), o.OrderNo, settledCapability)
	assertCode(t, err, xerr.InvalidAccess)
}

// A password change or reset revokes the account's sessions; a capability
// from before the revocation must not mint a new one. A revocation that
// predates the settlement, on an account that existed before the order,
// does not count.
func TestGuestStatusRefusesTheCapabilityAfterARevocation(t *testing.T) {
	f := newPortalFixture(t)
	u, o := f.settledGuestOrder()
	if err := usersession.Revoke(context.Background(), f.h.Redis, u.Id); err != nil {
		t.Fatal(err)
	}

	_, err := f.status(context.Background(), o.OrderNo, settledCapability)
	assertCode(t, err, xerr.InvalidAccess)

	f.settledAt(o.OrderNo, time.Now().Add(time.Second))
	resp, err := f.status(context.Background(), o.OrderNo, settledCapability)
	if err != nil || resp.Token == "" {
		t.Fatalf("a revocation before the settlement refused the exchange: %+v, %v", resp, err)
	}
	f.assertSessionOf(resp.Token, u)
}

// The authenticated owner holds a session already and is not exchanging a
// capability: the status answer stays available to them.
func TestGuestStatusForTheOwnerIsNotBoundByTheExchangeWindow(t *testing.T) {
	f := newPortalFixture(t)
	u, o := f.settledGuestOrder()
	f.settledAt(o.OrderNo, time.Now().Add(-2*GuestSessionExchangeWindow))

	resp, err := f.status(billingtest.UserContext(u), o.OrderNo, "")
	if err != nil || resp.Token == "" {
		t.Fatalf("owner QueryPurchaseOrder = (%+v, %v), want the status with a session", resp, err)
	}
	_, err = f.status(context.Background(), o.OrderNo, settledCapability)
	assertCode(t, err, xerr.InvalidAccess)
}
