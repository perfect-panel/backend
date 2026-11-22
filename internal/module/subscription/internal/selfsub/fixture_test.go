package selfsub

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"gorm.io/gorm"
)

// fixture is the subscription fixture with the billing side of a
// cancellation: the orders a quote reads in memory and the refund stage the
// billing module settles, recorded; the cancellation markers are in the
// fixture database.
type fixture struct {
	*subtest.Fixture
	svc     *Service
	orders  orders
	refunds *refunds
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logtest.Discard(t)
	f := &fixture{Fixture: subtest.New(t), orders: orders{}, refunds: &refunds{settled: map[int64]refund{}}}
	f.svc = NewService(Deps{
		UserSubs:    f.Store.UserSubscription(),
		Plans:       f.Store.Subscribe(),
		Orders:      f.orders,
		Refunds:     f.refunds,
		Cache:       f.Store.UserSubscription(),
		Logs:        subtest.NewLogs(f.DB),
		Inbox:       subtest.NewInbox(f.DB),
		Store:       f.Store,
		SingleModel: func() bool { return false },
	})
	return f
}

// as returns a request context authenticated as the user.
func as(userID int64) context.Context {
	return user.NewContext(context.Background(), &user.User{Id: userID})
}

// cancelMarker returns the result of the subscription's cancellation marker,
// or false without one.
func (f *fixture) cancelMarker(t *testing.T, subID int64) (string, bool) {
	t.Helper()
	record, err := subtest.NewInbox(f.DB).Find(context.Background(), unsubscribeCancelConsumer, fmt.Sprint(subID))
	if err != nil {
		t.Fatal(err)
	}
	if record == nil {
		return "", false
	}
	return record.Result, true
}

// cancelled records a cancellation whose refund was never settled, as a
// crash between the two stages leaves it.
func (f *fixture) cancelled(t *testing.T, subID int64, result string) {
	t.Helper()
	if err := subtest.NewInbox(f.DB).Insert(context.Background(), unsubscribeCancelConsumer, fmt.Sprint(subID), result); err != nil {
		t.Fatal(err)
	}
}

// orders is the billing read port.
type orders map[int64]*order.Details

var _ OrderReader = orders{}

func (o orders) FindOneDetails(_ context.Context, id int64) (*order.Details, error) {
	if details, ok := o[id]; ok {
		return details, nil
	}
	return nil, gorm.ErrRecordNotFound
}

var (
	errWalletUnavailable = errors.New("wallet unavailable")
	errAlreadySettled    = errors.New("refund already settled")
)

// refund is one settlement the refund stage requested.
type refund struct {
	userID, subID, orderID, amount int64
}

// refunds is the billing port of the refund stage. It records every
// settlement requested and reports a subscription settled once one
// succeeded; a second settlement fails like the billing marker makes it.
type refunds struct {
	settled  map[int64]refund
	requests []refund
	// failNext fails that many settlements, which then leave nothing
	// settled.
	failNext int
}

var _ RefundSettler = (*refunds)(nil)

func (r *refunds) UnsubscribeRefundSettled(_ context.Context, subID int64) (bool, error) {
	_, ok := r.settled[subID]
	return ok, nil
}

func (r *refunds) SettleUnsubscribeRefund(_ context.Context, userID, subID, orderID, amount int64) error {
	request := refund{userID: userID, subID: subID, orderID: orderID, amount: amount}
	r.requests = append(r.requests, request)
	if r.failNext > 0 {
		r.failNext--
		return errWalletUnavailable
	}
	if _, ok := r.settled[subID]; ok {
		return errAlreadySettled
	}
	r.settled[subID] = request
	return nil
}
