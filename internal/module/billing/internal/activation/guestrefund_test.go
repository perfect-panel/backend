package activation

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// guestAccountsFake is the identity port of the guest account stage; it can
// refuse the creation as identity refuses an identifier that gained an
// account meanwhile.
type guestAccountsFake struct {
	creates   int
	createErr error
}

func (g *guestAccountsFake) FindGuestAccount(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}

func (g *guestAccountsFake) EnsureGuestAccount(context.Context, identity.GuestAccountCommand) (int64, error) {
	g.creates++
	if g.createErr != nil {
		return 0, g.createErr
	}
	return 99, nil
}

// noFulfillment fails the test when the workflow reaches the fulfillment of
// an order it should have refunded.
type noFulfillment struct{ t *testing.T }

func (n noFulfillment) FulfillPaidOrder(context.Context, string) (*subscription.FulfillmentOutcome, error) {
	n.t.Fatal("a guest order without an account of its own reached fulfillment")
	return nil, nil
}

// guestRefundFixture is a paid guest order whose identity already belongs to
// an account holding a wallet.
type guestRefundFixture struct {
	h        *billingtest.Harness
	guests   *guestAccountsFake
	workflow *Workflow
	owner    *user.User
	guest    *order.Order
}

func newGuestRefundFixture(t *testing.T, guests *guestAccountsFake) *guestRefundFixture {
	t.Helper()
	h := billingtest.New(t)
	owner := h.User()
	if err := h.DB.Create(&user.AuthMethods{UserId: owner.Id, AuthType: "email", AuthIdentifier: "guest@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	h.Wallet(owner.Id, 500, 50)
	plan := h.Plan(1000)
	method := h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`)
	guest := h.Order(&order.Order{
		OrderNo: "guest-1", Type: order.TypeSubscribe, Status: order.StatusPaid, Price: 1000, Amount: 1000, GiftAmount: 0,
		PaymentId: method.Id, Method: method.Platform, TradeNo: "trade-guest-1", SubscribeId: plan.Id, IsNew: true,
		GuestAuthType: "email", GuestIdentifier: "guest@example.com", GuestPasswordHash: "hash",
	})
	stages := NewService(Deps{Orders: h.Store.Order(), Store: h.Store, Profiles: h.Store.User(), InvitePolicy: func() (uint8, bool) { return 0, false }})
	workflow := NewWorkflow(WorkflowDeps{
		Orders: h.Store.Order(), Profiles: h.Store.User(), GuestAccounts: guests, GuestIdentities: h.Store.UserAuth(),
		Subscriptions: noFulfillment{t: t},
	}, stages)
	return &guestRefundFixture{h: h, guests: guests, workflow: workflow, owner: owner, guest: guest}
}

// assertRefundedToOwner checks the one refund: the order bound to the
// identity's account and closed, the payment on that account's balance with
// its ledger entry, and no account created for the order.
func (f *guestRefundFixture) assertRefundedToOwner(t *testing.T) {
	t.Helper()
	closed := f.h.ReloadOrder(f.guest.OrderNo)
	if closed.Status != order.StatusClosed || closed.UserId != f.owner.Id {
		t.Fatalf("order = %+v, want it closed and bound to user %d", closed, f.owner.Id)
	}
	if w := f.h.ReloadWallet(f.owner.Id); w.Balance != 1500 || w.GiftAmount != 50 {
		t.Fatalf("owner wallet = %+v, want the 1000 paid returned to the balance once", w)
	}
	if logs := f.h.BalanceLogs(f.owner.Id); len(logs) != 1 || logs[0].Type != logEntity.BalanceTypeRefund || logs[0].Amount != 1000 || logs[0].OrderNo != f.guest.OrderNo {
		t.Fatalf("balance ledger = %+v, want one refund of 1000", logs)
	}
	if f.guests.creates != 0 {
		t.Fatalf("accounts created = %d, want none for an identity that has one", f.guests.creates)
	}
	paid, err := f.h.Store.Order().QueryOrdersByStatusAfterID(context.Background(), order.StatusPaid, 0, 10)
	if err != nil || len(paid) != 0 {
		t.Fatalf("paid orders = %v, %v; want none left for the reconciler", paid, err)
	}
}

// A guest identity may hold several pending orders; paying a second one
// after the first created the account left it Paid forever, since the
// account stage always found the identity taken. The payment is returned to
// the identity's account and the order closed; a redelivery changes nothing.
func TestActivateRefundsAGuestOrderWhoseIdentityHasAnAccount(t *testing.T) {
	f := newGuestRefundFixture(t, &guestAccountsFake{})
	for range 2 {
		if err := f.workflow.Activate(context.Background(), f.guest.OrderNo); err != nil {
			t.Fatalf("Activate: %v", err)
		}
	}
	f.assertRefundedToOwner(t)
}

// Identity may find the identifier taken only when it creates the account
// (a registration between the check and the creation); the refusal is
// resolved to the account it names.
func TestActivateRefundsAGuestOrderIdentityRefusedToCreate(t *testing.T) {
	f := newGuestRefundFixture(t, &guestAccountsFake{createErr: xerr.NewErrCode(xerr.UserExist)})
	// The identity reader finds the account only after identity refused.
	if err := f.h.DB.Where("user_id = ?", f.owner.Id).Delete(&user.AuthMethods{}).Error; err != nil {
		t.Fatal(err)
	}
	guests := f.guests
	f.workflow.deps.GuestIdentities = registeringIdentities{h: f.h, owner: f.owner, guests: guests, inner: f.workflow.deps.GuestIdentities}

	if err := f.workflow.Activate(context.Background(), f.guest.OrderNo); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	closed := f.h.ReloadOrder(f.guest.OrderNo)
	if closed.Status != order.StatusClosed || closed.UserId != f.owner.Id || guests.creates != 1 {
		t.Fatalf("order = %+v after %d creation attempts, want it refunded to user %d after identity's refusal", closed, guests.creates, f.owner.Id)
	}
	if w := f.h.ReloadWallet(f.owner.Id); w.Balance != 1500 {
		t.Fatalf("owner wallet = %+v, want the payment returned", w)
	}
}

// registeringIdentities registers the owner's identifier the moment identity
// refuses the creation, as a concurrent registration would have.
type registeringIdentities struct {
	h      *billingtest.Harness
	owner  *user.User
	guests *guestAccountsFake
	inner  interface {
		FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error)
	}
}

func (r registeringIdentities) FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error) {
	if r.guests.creates > 0 {
		if err := r.h.DB.Create(&user.AuthMethods{UserId: r.owner.Id, AuthType: method, AuthIdentifier: openID}).Error; err != nil {
			return nil, err
		}
	}
	return r.inner.FindUserAuthMethodByOpenID(ctx, method, openID)
}

// An order a payment provider collected is the provider's to settle, even
// when its identity is taken: billing holds none of its money.
func TestActivateDoesNotRefundAProviderCollectedGuestOrder(t *testing.T) {
	f := newGuestRefundFixture(t, &guestAccountsFake{})
	if err := f.h.DB.Model(&order.Order{}).Where("order_no = ?", f.guest.OrderNo).Update("method", "AppleIAP").Error; err != nil {
		t.Fatal(err)
	}
	var taken *guestAccountTaken
	if err := f.workflow.Activate(context.Background(), f.guest.OrderNo); !errors.As(err, &taken) {
		t.Fatalf("Activate = %v, want the taken identity reported", err)
	}
	if o := f.h.ReloadOrder(f.guest.OrderNo); o.Status != order.StatusPaid || o.UserId != 0 || f.h.ReloadWallet(f.owner.Id).Balance != 500 {
		t.Fatalf("order = %+v, want it left for the operator", o)
	}
}
