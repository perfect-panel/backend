package order

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	subscribeEntity "github.com/perfect-panel/server/internal/model/entity/subscribe"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"gorm.io/gorm"
)

type closeOrderStore struct {
	repository.Store
	orders     *closeOrderRepo
	subscribes *closeSubscribeRepo
	users      *closeUserRepo
	logs       *closeLogRepo
}

func (s *closeOrderStore) InTx(_ context.Context, fn func(repository.Store) error) error {
	return fn(s)
}
func (s *closeOrderStore) Order() repository.OrderRepo { return s.orders }
func (s *closeOrderStore) Subscribe() repository.SubscribeRepo {
	return s.subscribes
}
func (s *closeOrderStore) User() repository.UserRepo { return s.users }
func (s *closeOrderStore) Log() repository.LogRepo   { return s.logs }

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

func (r *closeSubscribeRepo) Update(_ context.Context, value *subscribeEntity.Subscribe, _ ...*gorm.DB) error {
	r.updateCalls++
	r.sub = value
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

func (r *closeUserRepo) Update(_ context.Context, value *userEntity.User, _ ...*gorm.DB) error {
	r.updateCalls++
	r.user = value
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
	logic := NewCloseOrderLogic(context.Background(), &svc.ServiceContext{Store: &closeOrderStore{orders: orders}})

	if err := logic.CloseOrder(&dto.CloseOrderRequest{OrderNo: "order-1"}); err != nil {
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
		order:      &orderEntity.Order{Id: 1, OrderNo: "guest-order", SubscribeId: 99, Status: 1},
		transition: true,
	}
	subscribes := &closeSubscribeRepo{sub: &subscribeEntity.Subscribe{Id: 99, Inventory: 2}}
	logic := NewCloseOrderLogic(context.Background(), &svc.ServiceContext{Store: &closeOrderStore{orders: orders, subscribes: subscribes}})

	if err := logic.CloseOrder(&dto.CloseOrderRequest{OrderNo: "guest-order"}); err != nil {
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
		order:      &orderEntity.Order{Id: 1, OrderNo: "gift-order", UserId: 7, GiftAmount: 40, SubscribeId: 99, Status: 1},
		transition: true,
	}
	subscribes := &closeSubscribeRepo{sub: &subscribeEntity.Subscribe{Id: 99, Inventory: 2}}
	users := &closeUserRepo{user: &userEntity.User{Id: 7, GiftAmount: 10}}
	logs := &closeLogRepo{}
	logic := NewCloseOrderLogic(context.Background(), &svc.ServiceContext{Store: &closeOrderStore{
		orders: orders, subscribes: subscribes, users: users, logs: logs,
	}})

	if err := logic.CloseOrder(&dto.CloseOrderRequest{OrderNo: "gift-order"}); err != nil {
		t.Fatalf("CloseOrder: %v", err)
	}
	if users.updateCalls != 1 || users.user.GiftAmount != 50 || logs.insertCalls != 1 {
		t.Fatalf("expected gift refund and log, updates=%d balance=%d logs=%d", users.updateCalls, users.user.GiftAmount, logs.insertCalls)
	}
	if subscribes.updateCalls != 1 || subscribes.sub.Inventory != 3 {
		t.Fatalf("expected inventory restoration after gift refund, calls=%d inventory=%d", subscribes.updateCalls, subscribes.sub.Inventory)
	}
}
