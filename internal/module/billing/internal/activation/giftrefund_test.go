package activation

import (
	"context"
	"fmt"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/billing/internal/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

// giftRefundFixture drives the flows a balance-paid order goes through over
// the real repositories: the buyer's order creation and close, the balance
// checkout, the unfulfillable refund and the cancellation refund.
type giftRefundFixture struct {
	t        *testing.T
	h        *billingtest.Harness
	checkout *checkout.Service
	portal   *portal.Service
	stages   *Service
	wallets  *wallet.Service
	buyer    *user.User
	ctx      context.Context
	plan     *subscribe.Subscribe
	balance  *payment.Payment
}

const (
	giftRefundPrice   int64 = 10000
	giftRefundBalance int64 = 10000
)

func newGiftRefundFixture(t *testing.T, gift int64) *giftRefundFixture {
	t.Helper()
	h := billingtest.New(t)
	queue := &billingtest.Queue{}
	currency := func() string { return "CNY" }
	f := &giftRefundFixture{t: t, h: h}
	f.checkout = checkout.NewService(checkout.Deps{
		Orders: h.Store.Order(), Coupons: h.Store.Coupon(), Payments: h.Store.Payment(), Plans: h.Store.Subscribe(),
		UserSubs: h.Store.UserSubscription(), Wallets: h.Store.Wallet(), Tx: h.Store, Inventory: subscription.NewInventory(h.Store),
		Queue: queue, SingleModel: func() bool { return false }, CurrencyUnit: currency,
	})
	f.portal = portal.NewService(portal.Deps{
		Orders: h.Store.Order(), OrderEvents: h.Store.OrderEvent(), Coupons: h.Store.Coupon(), Payments: h.Store.Payment(),
		UserAuths: h.Store.UserAuth(), Plans: h.Store.Subscribe(), Tx: h.Store, UserCache: &billingtest.UserCache{},
		Inventory: subscription.NewInventory(h.Store), Sessions: h.Redis, Queue: queue, GuestCheckoutCache: h.Redis,
		Config: portal.Config{SiteName: func() string { return "Panel" }, CurrencyUnit: currency, SiteHost: func() string { return "www.example.test" }},
	})
	f.stages = NewService(Deps{Orders: h.Store.Order(), Store: h.Store, Profiles: h.Store.User(), InvitePolicy: func() (uint8, bool) { return 0, false }})
	f.wallets = wallet.NewService(wallet.Deps{
		Logs: h.Store.Log(), Withdrawals: h.Store.UserWithdrawal(), Affiliates: h.Store.User(), AuthMethods: h.Store.UserAuth(),
		Tx: h.Store, Store: h.Store, Profiles: h.Store.User(),
	})
	f.buyer = h.User()
	h.Wallet(f.buyer.Id, giftRefundBalance, gift)
	f.ctx = billingtest.UserContext(f.buyer)
	f.plan = h.Plan(giftRefundPrice)
	f.balance = h.Payment("balance", "")
	return f
}

// wallet is the buyer's wallet.
func (f *giftRefundFixture) wallet() walletEntity.Wallet { return f.h.ReloadWallet(f.buyer.Id) }

// purchase opens a pending balance order of one plan unit.
func (f *giftRefundFixture) purchase() *order.Order {
	f.t.Helper()
	resp, err := f.checkout.Purchase(f.ctx, &dto.PurchaseOrderRequest{SubscribeId: f.plan.Id, Quantity: 1, Payment: f.balance.Id})
	if err != nil {
		f.t.Fatalf("Purchase: %v", err)
	}
	return f.h.ReloadOrder(resp.OrderNo)
}

// payLateGiftOrder reproduces the audit scenario: order B reserves the
// buyer's gift credit, order A is created without any, B is closed so the
// gift credit comes back, and A is then paid from the wallet, which spends
// the returned gift credit first. It returns A as paid.
func (f *giftRefundFixture) payLateGiftOrder(gift int64) *order.Order {
	f.t.Helper()
	b := f.purchase()
	a := f.purchase()
	if b.GiftAmount != min(gift, giftRefundPrice) || a.GiftAmount != 0 || a.Amount != giftRefundPrice {
		f.t.Fatalf("orders B = %+v A = %+v, want B holding the gift credit and A none", b, a)
	}
	if err := f.checkout.Close(f.ctx, &dto.CloseOrderRequest{OrderNo: b.OrderNo}); err != nil {
		f.t.Fatalf("Close B: %v", err)
	}
	if w := f.wallet(); w.Balance != giftRefundBalance || w.GiftAmount != gift {
		f.t.Fatalf("wallet after closing B = %+v, want the gift credit back", w)
	}
	resp, err := f.portal.Checkout(f.ctx, &dto.CheckoutOrderRequest{OrderNo: a.OrderNo})
	if err != nil || resp.Type != "balance" {
		f.t.Fatalf("Checkout A = (%+v, %v)", resp, err)
	}
	paid := f.h.ReloadOrder(a.OrderNo)
	giftUsed := min(gift, giftRefundPrice)
	if paid.Status != order.StatusPaid || paid.GiftAmount != giftUsed || paid.Amount != giftRefundPrice-giftUsed {
		f.t.Fatalf("paid A = %+v, want %d of gift credit and %d of balance, each counted once", paid, giftUsed, giftRefundPrice-giftUsed)
	}
	if w := f.wallet(); w.Balance != giftRefundBalance-(giftRefundPrice-giftUsed) || w.GiftAmount != gift-giftUsed {
		f.t.Fatalf("wallet after paying A = %+v", w)
	}
	return paid
}

// assertWholeAndNoMore checks that the buyer holds exactly what they started
// with: the refund paid back what was paid, once.
func (f *giftRefundFixture) assertWholeAndNoMore(gift int64) {
	f.t.Helper()
	if w := f.wallet(); w.Balance != giftRefundBalance || w.GiftAmount != gift {
		f.t.Fatalf("wallet after the refund = %+v, want balance %d and gift credit %d: the buyer must end where they started, never ahead", w, giftRefundBalance, gift)
	}
}

// Gift credit that arrives after an order was created and is spent at its
// balance checkout was counted twice (in Amount and in GiftAmount), and the
// refunds paid it back twice. The unfulfillable refund and the cancellation
// refund both return exactly what was paid, once.
func TestLateGiftCreditIsRefundedOnce(t *testing.T) {
	for _, gift := range []int64{giftRefundPrice, 4000, 0} {
		t.Run(fmt.Sprintf("gift=%d/unfulfillable refund", gift), func(t *testing.T) {
			f := newGiftRefundFixture(t, gift)
			paid := f.payLateGiftOrder(gift)

			if err := f.stages.RefundUnfulfillable(context.Background(), paid.OrderNo); err != nil {
				t.Fatalf("RefundUnfulfillable: %v", err)
			}
			f.assertWholeAndNoMore(gift)
			if f.h.ReloadOrder(paid.OrderNo).Status != order.StatusClosed {
				t.Fatal("the refunded order was not closed")
			}
			// A redelivery refunds nothing more.
			if err := f.stages.RefundUnfulfillable(context.Background(), paid.OrderNo); err != nil {
				t.Fatalf("replayed RefundUnfulfillable: %v", err)
			}
			f.assertWholeAndNoMore(gift)
		})
		t.Run(fmt.Sprintf("gift=%d/cancellation refund", gift), func(t *testing.T) {
			f := newGiftRefundFixture(t, gift)
			paid := f.payLateGiftOrder(gift)
			sub := f.h.UserSubscription(f.buyer.Id, f.plan, func(s *usersub.Subscribe) { s.OrderId = paid.Id })
			details, err := f.h.Store.Order().FindOneDetails(context.Background(), paid.Id)
			if err != nil || details.RefundBasis() != giftRefundPrice {
				t.Fatalf("RefundBasis = %d (%v), want the %d paid", details.RefundBasis(), err, giftRefundPrice)
			}

			// A cancellation recorded by the old formula could ask for more;
			// the settlement caps it at what was paid.
			if err := f.wallets.SettleUnsubscribeRefund(context.Background(), f.buyer.Id, sub.Id, paid.Id, 2*giftRefundPrice); err != nil {
				t.Fatalf("SettleUnsubscribeRefund: %v", err)
			}
			f.assertWholeAndNoMore(gift)
		})
	}
}
