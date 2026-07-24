package billing_test

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	paymentEntity "github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/repository"
	"gorm.io/gorm"
)

type fakeOrderRepo struct {
	repository.OrderRepo
	order        *orderEntity.Order
	details      *orderEntity.Details
	pendingCount int64
	inserted     *orderEntity.Order
	markedPaid   bool
	closed       bool
}

func (f *fakeOrderRepo) FindOneDetailsByOrderNo(_ context.Context, orderNo string) (*orderEntity.Details, error) {
	if f.details == nil || f.details.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *f.details
	return &copy, nil
}

func (f *fakeOrderRepo) FindOne(_ context.Context, id int64) (*orderEntity.Order, error) {
	if f.order == nil || f.order.Id != id {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *f.order
	return &copy, nil
}

func (f *fakeOrderRepo) FindOneByOrderNoForUpdate(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if f.order == nil || f.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *f.order
	return &copy, nil
}

func (f *fakeOrderRepo) Insert(_ context.Context, data *orderEntity.Order, _ ...*gorm.DB) error {
	f.inserted = data
	return nil
}

func (f *fakeOrderRepo) Update(_ context.Context, data *orderEntity.Order, _ ...*gorm.DB) error {
	f.order = data
	return nil
}

func (f *fakeOrderRepo) MarkOrderPaid(_ context.Context, orderNo, tradeNo string, _ ...*gorm.DB) (bool, error) {
	if f.order.OrderNo != orderNo || f.order.Status != 1 {
		return false, nil
	}
	f.order.Status = 2
	f.order.TradeNo = tradeNo
	f.markedPaid = true
	return true, nil
}

func (f *fakeOrderRepo) UpdateOrderStatusFrom(_ context.Context, orderNo string, from, to uint8, _ ...*gorm.DB) (bool, error) {
	if f.order.OrderNo != orderNo || f.order.Status != from {
		return false, nil
	}
	f.order.Status = to
	f.closed = true
	return true, nil
}

func (f *fakeOrderRepo) CountPendingByPaymentID(_ context.Context, _ int64) (int64, error) {
	return f.pendingCount, nil
}

type fakePaymentRepo struct {
	repository.PaymentRepo
	method  *paymentEntity.Payment
	deleted []int64
}

func (f *fakePaymentRepo) FindOne(_ context.Context, id int64) (*paymentEntity.Payment, error) {
	if f.method == nil || f.method.Id != id {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *f.method
	return &copy, nil
}

func (f *fakePaymentRepo) Delete(_ context.Context, id int64, _ ...*gorm.DB) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeBillingTx struct {
	orders   *fakeOrderRepo
	payments *fakePaymentRepo
}

func (f fakeBillingTx) InBillingTx(_ context.Context, fn func(repository.BillingStore) error) error {
	return fn(billingStoreView{orders: f.orders, payments: f.payments})
}

// billingStoreView satisfies repository.BillingStore for the fakes.
type billingStoreView struct {
	repository.BillingStore
	orders   *fakeOrderRepo
	payments *fakePaymentRepo
}

func (v billingStoreView) Order() repository.OrderRepo     { return v.orders }
func (v billingStoreView) Payment() repository.PaymentRepo { return v.payments }

type fakeActivationQueue struct {
	enqueued []string
}

func (f *fakeActivationQueue) EnqueueActivation(_ context.Context, orderNo string) error {
	f.enqueued = append(f.enqueued, orderNo)
	return nil
}

func (f *fakeActivationQueue) EnqueueDeferredClose(_ context.Context, _ string) error { return nil }

type billingFakes struct {
	orders   *fakeOrderRepo
	payments *fakePaymentRepo
	queue    *fakeActivationQueue
}

func newBillingService(orders *fakeOrderRepo, payments *fakePaymentRepo) (billing.Service, *billingFakes) {
	fakes := &billingFakes{orders: orders, payments: payments, queue: &fakeActivationQueue{}}
	svc := billing.New(billing.Deps{
		Orders:   orders,
		Payments: payments,
		Tx:       fakeBillingTx{orders: orders, payments: payments},
		Queue:    fakes.queue,
		Host:     "panel.example.com",
	})
	return svc, fakes
}

func newBillingServiceWithCoupons(coupons *fakeCouponRepo) billing.Service {
	return billing.New(billing.Deps{
		Orders:   &fakeOrderRepo{},
		Payments: &fakePaymentRepo{},
		Coupons:  coupons,
		Queue:    &fakeActivationQueue{},
	})
}

func TestUpdateOrderStatusRejectsInvalidTransitions(t *testing.T) {
	orders := &fakeOrderRepo{order: &orderEntity.Order{Id: 1, OrderNo: "o-1", Status: 1}}
	svc, fakes := newBillingService(orders, &fakePaymentRepo{})

	for _, req := range []*dto.UpdateOrderStatusRequest{
		{Id: 1, Status: 5, TradeNo: "t"},               // arbitrary terminal state
		{Id: 1, Status: 2},                             // paid without trade number
		{Id: 1, Status: 3, TradeNo: "t"},               // close with payment fields
		{Id: 1, Status: 3, PaymentId: 9},               // close with payment fields
		{Id: 1, Status: 1, TradeNo: "t", PaymentId: 0}, // no-op transition
	} {
		if err := svc.UpdateOrderStatus(context.Background(), req); err == nil {
			t.Fatalf("transition %+v must be rejected", req)
		}
	}
	if orders.order.Status != 1 || len(fakes.queue.enqueued) != 0 {
		t.Fatalf("rejected transitions must not mutate state: %+v", orders.order)
	}
}

func TestUpdateOrderStatusMarksPaidAndEnqueuesActivation(t *testing.T) {
	orders := &fakeOrderRepo{order: &orderEntity.Order{Id: 1, OrderNo: "o-2", Status: 1}}
	svc, fakes := newBillingService(orders, &fakePaymentRepo{})

	if err := svc.UpdateOrderStatus(context.Background(), &dto.UpdateOrderStatusRequest{Id: 1, Status: 2, TradeNo: "trade-1"}); err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	if !orders.markedPaid || orders.order.TradeNo != "trade-1" {
		t.Fatalf("order not marked paid: %+v", orders.order)
	}
	if len(fakes.queue.enqueued) != 1 || fakes.queue.enqueued[0] != "o-2" {
		t.Fatalf("activation not enqueued: %v", fakes.queue.enqueued)
	}
}

func TestUpdateOrderStatusCloseDoesNotEnqueue(t *testing.T) {
	orders := &fakeOrderRepo{order: &orderEntity.Order{Id: 1, OrderNo: "o-3", Status: 1}}
	svc, fakes := newBillingService(orders, &fakePaymentRepo{})

	if err := svc.UpdateOrderStatus(context.Background(), &dto.UpdateOrderStatusRequest{Id: 1, Status: 3}); err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	if !orders.closed {
		t.Fatalf("order not closed: %+v", orders.order)
	}
	if len(fakes.queue.enqueued) != 0 {
		t.Fatal("closing must not enqueue activation")
	}
}

func TestDeletePaymentMethodGuardsPendingOrders(t *testing.T) {
	orders := &fakeOrderRepo{pendingCount: 2}
	payments := &fakePaymentRepo{method: &paymentEntity.Payment{Id: 5}}
	svc, _ := newBillingService(orders, payments)

	if err := svc.DeletePaymentMethod(context.Background(), &dto.DeletePaymentMethodRequest{Id: 5}); err == nil {
		t.Fatal("deleting a payment method with pending orders must be rejected")
	}
	if len(payments.deleted) != 0 {
		t.Fatal("payment method must not be deleted")
	}

	orders.pendingCount = 0
	if err := svc.DeletePaymentMethod(context.Background(), &dto.DeletePaymentMethodRequest{Id: 5}); err != nil {
		t.Fatalf("DeletePaymentMethod: %v", err)
	}
	if len(payments.deleted) != 1 {
		t.Fatal("payment method deletion missing")
	}
}

func TestCreatePaymentMethodValidatesFeeAndPlatform(t *testing.T) {
	svc, _ := newBillingService(&fakeOrderRepo{}, &fakePaymentRepo{})

	if _, err := svc.CreatePaymentMethod(context.Background(), &dto.CreatePaymentMethodRequest{Platform: "Nope"}); err == nil {
		t.Fatal("unsupported platform must be rejected")
	}
	if _, err := svc.CreatePaymentMethod(context.Background(), &dto.CreatePaymentMethodRequest{Platform: "EPay", FeeMode: 9}); err == nil {
		t.Fatal("invalid fee mode must be rejected")
	}
}
