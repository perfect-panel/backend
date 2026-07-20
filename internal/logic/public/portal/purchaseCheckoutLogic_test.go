package portal

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"gorm.io/gorm"
)

type balancePaymentStore struct {
	repository.Store
	orders *balancePaymentOrderRepo
	users  *balancePaymentUserRepo
	logs   *balancePaymentLogRepo
}

func (s *balancePaymentStore) InTx(ctx context.Context, fn func(repository.Store) error) error {
	return fn(s)
}

func (s *balancePaymentStore) Order() repository.OrderRepo { return s.orders }
func (s *balancePaymentStore) User() repository.UserRepo   { return s.users }
func (s *balancePaymentStore) Log() repository.LogRepo     { return s.logs }

type balancePaymentOrderRepo struct {
	repository.OrderRepo
	order *orderEntity.Order
}

func (r *balancePaymentOrderRepo) FindOneByOrderNoForUpdate(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if r.order.OrderNo != orderNo {
		return nil, stderrors.New("unexpected order")
	}
	locked := *r.order
	return &locked, nil
}

func (r *balancePaymentOrderRepo) Update(_ context.Context, data *orderEntity.Order, _ ...*gorm.DB) error {
	r.order.GiftAmount = data.GiftAmount
	return nil
}

func (r *balancePaymentOrderRepo) UpdateOrderStatusFrom(_ context.Context, orderNo string, from, status uint8, _ ...*gorm.DB) (bool, error) {
	if r.order.OrderNo != orderNo {
		return false, stderrors.New("unexpected order")
	}
	if r.order.Status != from {
		return false, nil
	}
	r.order.Status = status
	return true, nil
}

type balancePaymentUserRepo struct {
	repository.UserRepo
	user *userEntity.User
}

func (r *balancePaymentUserRepo) FindOneForUpdate(_ context.Context, id int64) (*userEntity.User, error) {
	if r.user.Id != id {
		return nil, stderrors.New("unexpected user")
	}
	locked := *r.user
	return &locked, nil
}

func (r *balancePaymentUserRepo) UpdateBalanceFields(_ context.Context, data *userEntity.User, _ ...*gorm.DB) error {
	r.user.Balance = data.Balance
	r.user.GiftAmount = data.GiftAmount
	return nil
}

func (r *balancePaymentUserRepo) ClearUserCache(_ context.Context, _ ...*userEntity.User) error {
	return nil
}

type balancePaymentLogRepo struct {
	repository.LogRepo
	logs []*logEntity.SystemLog
}

func (r *balancePaymentLogRepo) Insert(_ context.Context, data *logEntity.SystemLog) error {
	r.logs = append(r.logs, data)
	return nil
}

func newBalancePaymentLogic(t *testing.T, store *balancePaymentStore) *PurchaseCheckoutLogic {
	t.Helper()
	redisServer := miniredis.RunT(t)
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = queue.Close() })
	return NewPurchaseCheckoutLogic(context.Background(), &svc.ServiceContext{
		Store: store,
		Queue: queue,
	})
}

func TestBalancePaymentRejectsInsufficientCurrentBalance(t *testing.T) {
	store := &balancePaymentStore{
		orders: &balancePaymentOrderRepo{order: &orderEntity.Order{OrderNo: "order-1", UserId: 10, Amount: 2500, Status: 1}},
		users:  &balancePaymentUserRepo{user: &userEntity.User{Id: 10, Balance: 500}},
		logs:   &balancePaymentLogRepo{},
	}
	logic := newBalancePaymentLogic(t, store)

	err := logic.balancePayment(store.users.user, store.orders.order)
	if err == nil || !strings.Contains(err.Error(), "Insufficient balance") {
		t.Fatalf("balancePayment error = %v, want insufficient balance", err)
	}
	if store.users.user.Balance != 500 || store.users.user.GiftAmount != 0 {
		t.Fatalf("user balance changed: %+v", store.users.user)
	}
	if store.orders.order.Status != 1 {
		t.Fatalf("order status = %d, want pending", store.orders.order.Status)
	}
	if len(store.logs.logs) != 0 {
		t.Fatalf("unexpected logs: %d", len(store.logs.logs))
	}
}

func TestBalancePaymentAddsCheckoutGiftToExistingOrderGift(t *testing.T) {
	store := &balancePaymentStore{
		orders: &balancePaymentOrderRepo{order: &orderEntity.Order{OrderNo: "order-2", UserId: 10, Amount: 2500, GiftAmount: 300, Status: 1}},
		users:  &balancePaymentUserRepo{user: &userEntity.User{Id: 10, Balance: 2300, GiftAmount: 200}},
		logs:   &balancePaymentLogRepo{},
	}
	logic := newBalancePaymentLogic(t, store)

	if err := logic.balancePayment(store.users.user, store.orders.order); err != nil {
		t.Fatalf("balancePayment: %v", err)
	}
	if store.users.user.Balance != 0 || store.users.user.GiftAmount != 0 {
		t.Fatalf("user balance = %+v, want zero balances", store.users.user)
	}
	if store.orders.order.GiftAmount != 500 {
		t.Fatalf("order gift amount = %d, want 500", store.orders.order.GiftAmount)
	}
	if store.orders.order.Status != 2 {
		t.Fatalf("order status = %d, want paid", store.orders.order.Status)
	}
	if len(store.logs.logs) != 2 {
		t.Fatalf("logs = %d, want gift and balance logs", len(store.logs.logs))
	}
}

func TestBalancePaymentDoesNotDebitNonPendingOrder(t *testing.T) {
	store := &balancePaymentStore{
		orders: &balancePaymentOrderRepo{order: &orderEntity.Order{OrderNo: "order-3", UserId: 10, Amount: 2500, Status: 2}},
		users:  &balancePaymentUserRepo{user: &userEntity.User{Id: 10, Balance: 2500}},
		logs:   &balancePaymentLogRepo{},
	}
	logic := newBalancePaymentLogic(t, store)

	err := logic.balancePayment(store.users.user, store.orders.order)
	if err == nil || !strings.Contains(err.Error(), "order is no longer pending") {
		t.Fatalf("balancePayment error = %v, want order status error", err)
	}
	if store.users.user.Balance != 2500 || store.users.user.GiftAmount != 0 {
		t.Fatalf("user balance changed: %+v", store.users.user)
	}
	if len(store.logs.logs) != 0 {
		t.Fatalf("unexpected logs: %d", len(store.logs.logs))
	}
}
