package orderLogic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	subscribeEntity "github.com/perfect-panel/server/internal/model/entity/subscribe"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
	"gorm.io/gorm"
)

type activationStore struct {
	repository.Store
	orders *activationOrderRepo
	users  *activationUserRepo
	logs   *activationLogRepo
}

func (s *activationStore) InTx(_ context.Context, fn func(repository.Store) error) error {
	return fn(s)
}
func (s *activationStore) Order() repository.OrderRepo { return s.orders }
func (s *activationStore) User() repository.UserRepo   { return s.users }
func (s *activationStore) Log() repository.LogRepo     { return s.logs }

type activationOrderRepo struct {
	repository.OrderRepo
	order *orderEntity.Order
}

func (r *activationOrderRepo) FindOneByOrderNoForUpdate(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if r.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.order
	return &copy, nil
}

func (r *activationOrderRepo) UpdateOrderStatusFrom(_ context.Context, orderNo string, from, to uint8, _ ...*gorm.DB) (bool, error) {
	if r.order.OrderNo != orderNo || r.order.Status != from {
		return false, nil
	}
	r.order.Status = to
	return true, nil
}

type activationUserRepo struct {
	repository.UserRepo
	user             *userEntity.User
	updateCacheCalls int
	quotaCount       int64
	quotaCountCalls  int
	blocking         bool
	hasBlockingCalls int
}

func (r *activationUserRepo) FindOneForUpdate(_ context.Context, id int64) (*userEntity.User, error) {
	if r.user.Id != id {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *r.user
	return &copy, nil
}

func (r *activationUserRepo) Update(_ context.Context, data *userEntity.User, _ ...*gorm.DB) error {
	r.user.Balance = data.Balance
	return nil
}

func (r *activationUserRepo) UpdateBalanceFields(_ context.Context, data *userEntity.User, _ ...*gorm.DB) error {
	r.user.Balance = data.Balance
	r.user.GiftAmount = data.GiftAmount
	return nil
}

func (r *activationUserRepo) UpdateUserCache(_ context.Context, _ *userEntity.User) error {
	r.updateCacheCalls++
	return nil
}

func (r *activationUserRepo) CountQuotaConsumingSubscriptions(_ context.Context, _ int64, _ int64) (int64, error) {
	r.quotaCountCalls++
	return r.quotaCount, nil
}

func (r *activationUserRepo) HasBlockingSubscription(_ context.Context, _ int64) (bool, error) {
	r.hasBlockingCalls++
	return r.blocking, nil
}

type activationLogRepo struct {
	repository.LogRepo
	logs []*logEntity.SystemLog
}

func (r *activationLogRepo) Insert(_ context.Context, data *logEntity.SystemLog) error {
	r.logs = append(r.logs, data)
	return nil
}

func TestActivateRechargeCommitsSettlementOnlyOnce(t *testing.T) {
	store := &activationStore{
		orders: &activationOrderRepo{order: &orderEntity.Order{
			OrderNo: "recharge-order", UserId: 7, Type: OrderTypeRecharge, Price: 1250, Status: OrderStatusPaid,
		}},
		users: &activationUserRepo{user: &userEntity.User{Id: 7, Balance: 500}},
		logs:  &activationLogRepo{},
	}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store})
	payload, err := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: "recharge-order"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(types.ForthwithActivateOrder, payload)

	if err := logic.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	if err := logic.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("duplicate activation: %v", err)
	}
	if store.orders.order.Status != OrderStatusFinished {
		t.Fatalf("order status = %d, want finished", store.orders.order.Status)
	}
	if store.users.user.Balance != 1750 {
		t.Fatalf("balance = %d, want 1750", store.users.user.Balance)
	}
	if len(store.logs.logs) != 1 {
		t.Fatalf("recharge logs = %d, want 1", len(store.logs.logs))
	}
}

func TestCreateUserSubscriptionTxEnforcesQuota(t *testing.T) {
	users := &activationUserRepo{quotaCount: 1}
	store := &activationStore{users: users}
	logic := NewActivateOrderLogic(&svc.ServiceContext{})

	_, err := logic.createUserSubscriptionTx(context.Background(), store, &orderEntity.Order{UserId: 7, SubscribeId: 9}, &subscribeEntity.Subscribe{Quota: 1})
	if err == nil {
		t.Fatal("activation created a subscription after quota was exhausted")
	}
	if users.quotaCountCalls != 1 {
		t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
	}
}

func TestCreateUserSubscriptionTxEnforcesSingleModel(t *testing.T) {
	users := &activationUserRepo{blocking: true}
	store := &activationStore{users: users}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Config: config.Config{Subscribe: config.SubscribeConfig{SingleModel: true}}})

	_, err := logic.createUserSubscriptionTx(context.Background(), store, &orderEntity.Order{UserId: 7, SubscribeId: 9}, &subscribeEntity.Subscribe{})
	if err == nil {
		t.Fatal("activation created a subscription despite a blocking subscription")
	}
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}
