package portal

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/dto"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
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
func (s *balancePaymentStore) UserCache() repository.UserCacheRepo {
	return s.users
}
func (s *balancePaymentStore) Log() repository.LogRepo { return s.logs }

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
	repository.UserCacheRepo
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
	return NewPurchaseCheckoutLogic(context.Background(), CheckoutDependencies{
		Store:           NewCheckoutStore(store),
		ActivationQueue: queue,
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

func TestAuthorizeCheckoutRequiresOwnerForUserOrder(t *testing.T) {
	logic := NewPurchaseCheckoutLogic(
		context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 7}),
		CheckoutDependencies{},
	)
	err := logic.authorizeCheckout(&orderEntity.Order{OrderNo: "order-1", UserId: 8}, &dto.CheckoutOrderRequest{OrderNo: "order-1"})
	if err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("authorizeCheckout error = %v, want owner mismatch", err)
	}
}

func TestAuthorizeCheckoutValidatesGuestCheckoutToken(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	info := constant.TemporaryOrderInfo{OrderNo: "guest-order", CheckoutToken: "secure-checkout-token"}
	encoded, err := info.Marshal()
	if err != nil {
		t.Fatalf("marshal temporary order: %v", err)
	}
	if err := client.Set(context.Background(), fmt.Sprintf(constant.TempOrderCacheKey, info.OrderNo), encoded, 0).Err(); err != nil {
		t.Fatalf("store temporary order: %v", err)
	}

	logic := NewPurchaseCheckoutLogic(context.Background(), CheckoutDependencies{GuestCheckoutCache: client})
	orderInfo := &orderEntity.Order{OrderNo: info.OrderNo}
	if err := logic.authorizeCheckout(orderInfo, &dto.CheckoutOrderRequest{OrderNo: info.OrderNo, CheckoutToken: info.CheckoutToken}); err != nil {
		t.Fatalf("valid guest checkout rejected: %v", err)
	}
	if err := logic.authorizeCheckout(orderInfo, &dto.CheckoutOrderRequest{OrderNo: info.OrderNo, CheckoutToken: "wrong-token"}); err == nil {
		t.Fatal("invalid guest checkout token was accepted")
	}
}

type paymentExpectationStore struct {
	CheckoutStore
	order       *orderEntity.Order
	updateCalls int
}

func (s *paymentExpectationStore) FindOrderByOrderNo(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if s.order == nil || s.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *s.order
	return &copy, nil
}

func (s *paymentExpectationStore) UpdatePaymentExpectation(_ context.Context, orderNo string, amount int64, currency string) (bool, error) {
	s.updateCalls++
	if s.order == nil || s.order.OrderNo != orderNo || s.order.Status != 1 || s.order.PaymentCurrency != "" {
		return false, nil
	}
	s.order.PaymentAmount = amount
	s.order.PaymentCurrency = currency
	return true, nil
}

func TestPersistPaymentExpectationReusesMatchingSnapshot(t *testing.T) {
	store := &paymentExpectationStore{order: &orderEntity.Order{
		OrderNo: "order-1", Status: 1, PaymentAmount: 1250, PaymentCurrency: "CNY", TradeNo: "pi_123",
	}}
	logic := NewPurchaseCheckoutLogic(context.Background(), CheckoutDependencies{Store: store})
	staleOrder := &orderEntity.Order{OrderNo: "order-1", Status: 1}

	if err := logic.persistPaymentExpectation(staleOrder, 1250, "cny"); err != nil {
		t.Fatalf("persist matching expectation: %v", err)
	}
	if store.updateCalls != 1 {
		t.Fatalf("UpdatePaymentExpectation calls = %d, want 1", store.updateCalls)
	}
	if staleOrder.PaymentAmount != 1250 || staleOrder.PaymentCurrency != "CNY" || staleOrder.TradeNo != "pi_123" {
		t.Fatalf("reloaded order = %+v, want stored payment snapshot and trade number", staleOrder)
	}
}

func TestPersistPaymentExpectationRejectsDifferentSnapshot(t *testing.T) {
	store := &paymentExpectationStore{order: &orderEntity.Order{
		OrderNo: "order-1", Status: 1, PaymentAmount: 1250, PaymentCurrency: "CNY",
	}}
	logic := NewPurchaseCheckoutLogic(context.Background(), CheckoutDependencies{Store: store})
	staleOrder := &orderEntity.Order{OrderNo: "order-1", Status: 1}

	err := logic.persistPaymentExpectation(staleOrder, 1300, "CNY")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("persist mismatched expectation error = %v, want mismatch", err)
	}
}
