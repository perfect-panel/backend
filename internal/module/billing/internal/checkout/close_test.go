package checkout

import (
	"context"
	"fmt"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	inboxEntity "github.com/perfect-panel/server/internal/model/entity/inbox"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	subscribeEntity "github.com/perfect-panel/server/internal/model/entity/subscribe"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"gorm.io/gorm"
)

type closeOrderStore struct {
	repository.Store
	orders     *closeOrderRepo
	subscribes *closeSubscribeRepo
	users      *closeUserRepo
	logs       *closeLogRepo
	inbox      *closeInboxRepo
}

func (s *closeOrderStore) InTx(_ context.Context, fn func(repository.Store) error) error {
	return fn(s)
}

func (s *closeOrderStore) InBillingTx(_ context.Context, fn func(repository.BillingStore) error) error {
	return fn(s)
}

func (s *closeOrderStore) InSubscriptionTx(_ context.Context, fn func(repository.SubscriptionStore) error) error {
	return fn(s)
}

func (s *closeOrderStore) Wallet() repository.WalletRepo { return s.users }
func (s *closeOrderStore) Order() repository.OrderRepo   { return s.orders }
func (s *closeOrderStore) Subscribe() repository.SubscribeRepo {
	return s.subscribes
}
func (s *closeOrderStore) User() repository.UserRepo { return s.users }
func (s *closeOrderStore) Log() repository.LogRepo   { return s.logs }
func (s *closeOrderStore) Inbox() repository.InboxRepo {
	if s.inbox == nil {
		s.inbox = &closeInboxRepo{records: map[string]string{}}
	}
	return s.inbox
}

// newCloseService wires the checkout service against the fake store; only the
// dependencies the close flow touches are provided.
func newCloseService(store *closeOrderStore) *Service {
	return NewService(Deps{
		Orders:   store.orders,
		Payments: nil, // gateway settlement is not exercised: fake orders carry no gateway method
		Store:    store,
	})
}

type closeInboxRepo struct {
	repository.InboxRepo
	records map[string]string
}

func (r *closeInboxRepo) Find(_ context.Context, consumer, key string) (*inboxEntity.Record, error) {
	result, ok := r.records[consumer+"|"+key]
	if !ok {
		return nil, nil
	}
	return &inboxEntity.Record{Consumer: consumer, EventKey: key, Result: result}, nil
}

func (r *closeInboxRepo) Insert(_ context.Context, consumer, key, result string) error {
	k := consumer + "|" + key
	if _, ok := r.records[k]; ok {
		return fmt.Errorf("duplicate inbox record %s", k)
	}
	r.records[k] = result
	return nil
}

// markReserved seeds the inbox as if the purchase flow had reserved inventory
// for the order (the new-flow invariant for pending subscribe orders).
func (s *closeOrderStore) markReserved(t *testing.T, orderNo string) {
	t.Helper()
	if err := s.Inbox().Insert(context.Background(), orderflow.InventoryReserveConsumer, orderNo, ""); err != nil {
		t.Fatalf("seed reserve marker: %v", err)
	}
}

type closeOrderRepo struct {
	repository.OrderRepo
	order       *orderEntity.Order
	transition  bool
	from        uint8
	to          uint8
	deleteCalls int
}

func (r *closeOrderRepo) FindOneByOrderNo(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if orderNo != r.order.OrderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.order
	return &copy, nil
}

func (r *closeOrderRepo) UpdateOrderStatusFrom(_ context.Context, orderNo string, from, to uint8, _ ...*gorm.DB) (bool, error) {
	r.from, r.to = from, to
	if orderNo != r.order.OrderNo || !r.transition {
		return false, nil
	}
	r.order.Status = to
	return true, nil
}

func (r *closeOrderRepo) Delete(_ context.Context, _ int64, _ ...*gorm.DB) error {
	r.deleteCalls++
	return nil
}

type closeSubscribeRepo struct {
	repository.SubscribeRepo
	sub         *subscribeEntity.Subscribe
	updateCalls int
}

func (r *closeSubscribeRepo) FindOne(_ context.Context, id int64) (*subscribeEntity.Subscribe, error) {
	if r.sub == nil || id != r.sub.Id {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.sub
	return &copy, nil
}

func (r *closeSubscribeRepo) RestoreInventory(_ context.Context, id int64, _ ...*gorm.DB) error {
	if r.sub == nil || r.sub.Id != id {
		return gorm.ErrRecordNotFound
	}
	if r.sub.Inventory != -1 {
		r.sub.Inventory++
	}
	r.updateCalls++
	return nil
}

type closeUserRepo struct {
	repository.UserRepo
	user        *userEntity.User
	updateCalls int
}

func (r *closeUserRepo) FindOne(_ context.Context, id int64) (*userEntity.User, error) {
	if r.user == nil || id != r.user.Id {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.user
	return &copy, nil
}

func (r *closeUserRepo) FindOneForUpdate(ctx context.Context, id int64) (*userEntity.User, error) {
	return r.FindOne(ctx, id)
}

func (r *closeUserRepo) UpdateBalanceFields(_ context.Context, value *userEntity.User, _ ...*gorm.DB) error {
	r.updateCalls++
	r.user.Balance = value.Balance
	r.user.GiftAmount = value.GiftAmount
	return nil
}

type closeLogRepo struct {
	repository.LogRepo
	insertCalls int
}

func (r *closeLogRepo) Insert(_ context.Context, _ *logEntity.SystemLog) error {
	r.insertCalls++
	return nil
}

func TestCloseOrderDoesNotOverwriteConcurrentPayment(t *testing.T) {
	orders := &closeOrderRepo{
		order:      &orderEntity.Order{Id: 1, OrderNo: "order-1", Status: 1},
		transition: false, // callback already transitioned Pending -> Paid
	}
	svc := newCloseService(&closeOrderStore{orders: orders})

	if err := svc.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: "order-1"}); err != nil {
		t.Fatalf("CloseOrder: %v", err)
	}
	if orders.from != 1 || orders.to != 3 {
		t.Fatalf("expected conditional Pending -> Closed transition, got %d -> %d", orders.from, orders.to)
	}
	if orders.deleteCalls != 0 {
		t.Fatal("guest order was deleted after conditional close lost the race")
	}
}

func TestCloseOrderRetainsGuestOrderAndRestoresInventory(t *testing.T) {
	orders := &closeOrderRepo{
		order:      &orderEntity.Order{Id: 1, OrderNo: "guest-order", Type: 1, SubscribeId: 99, Status: 1},
		transition: true,
	}
	subscribes := &closeSubscribeRepo{sub: &subscribeEntity.Subscribe{Id: 99, Inventory: 2}}
	store := &closeOrderStore{orders: orders, subscribes: subscribes}
	store.markReserved(t, "guest-order")
	svc := newCloseService(store)

	if err := svc.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: "guest-order"}); err != nil {
		t.Fatalf("CloseOrder: %v", err)
	}
	if orders.order.Status != 3 {
		t.Fatalf("expected closed status, got %d", orders.order.Status)
	}
	if orders.deleteCalls != 0 {
		t.Fatal("closed guest order must be retained for audit")
	}
	if subscribes.updateCalls != 1 || subscribes.sub.Inventory != 3 {
		t.Fatalf("expected guest close to restore inventory once, calls=%d inventory=%d", subscribes.updateCalls, subscribes.sub.Inventory)
	}
}

func TestCloseOrderRefundsGiftAndRestoresInventory(t *testing.T) {
	orders := &closeOrderRepo{
		order:      &orderEntity.Order{Id: 1, OrderNo: "gift-order", Type: 1, UserId: 7, GiftAmount: 40, SubscribeId: 99, Status: 1},
		transition: true,
	}
	subscribes := &closeSubscribeRepo{sub: &subscribeEntity.Subscribe{Id: 99, Inventory: 2}}
	users := &closeUserRepo{user: &userEntity.User{Id: 7, GiftAmount: 10}}
	logs := &closeLogRepo{}
	store := &closeOrderStore{orders: orders, subscribes: subscribes, users: users, logs: logs}
	store.markReserved(t, "gift-order")
	svc := newCloseService(store)

	if err := svc.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: "gift-order"}); err != nil {
		t.Fatalf("CloseOrder: %v", err)
	}
	if users.updateCalls != 1 || users.user.GiftAmount != 50 || logs.insertCalls != 1 {
		t.Fatalf("expected gift refund and log, updates=%d balance=%d logs=%d", users.updateCalls, users.user.GiftAmount, logs.insertCalls)
	}
	if subscribes.updateCalls != 1 || subscribes.sub.Inventory != 3 {
		t.Fatalf("expected inventory restoration after gift refund, calls=%d inventory=%d", subscribes.updateCalls, subscribes.sub.Inventory)
	}
}

func TestCloseOrderDoesNotRestoreInventoryForRenewalOrTrafficReset(t *testing.T) {
	for _, orderType := range []uint8{2, 3} {
		t.Run(fmt.Sprintf("type=%d", orderType), func(t *testing.T) {
			orders := &closeOrderRepo{
				order:      &orderEntity.Order{Id: 1, OrderNo: "existing-subscription-order", Type: orderType, SubscribeId: 99, Status: 1},
				transition: true,
			}
			subscribes := &closeSubscribeRepo{sub: &subscribeEntity.Subscribe{Id: 99, Inventory: 2}}
			svc := newCloseService(&closeOrderStore{orders: orders, subscribes: subscribes})

			if err := svc.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: "existing-subscription-order"}); err != nil {
				t.Fatalf("CloseOrder: %v", err)
			}
			if orders.order.Status != 3 {
				t.Fatalf("status = %d, want closed", orders.order.Status)
			}
			if subscribes.updateCalls != 0 || subscribes.sub.Inventory != 2 {
				t.Fatalf("renewal/reset close must not restore inventory, calls=%d inventory=%d", subscribes.updateCalls, subscribes.sub.Inventory)
			}
		})
	}
}
