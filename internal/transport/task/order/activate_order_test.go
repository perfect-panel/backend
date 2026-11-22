package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/billing"
	orderEntity "github.com/perfect-panel/backend/internal/module/billing/entity/order"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	userEntity "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	inboxEntity "github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
	subscribeEntity "github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"gorm.io/gorm"
)

// activationModules are the real facades a saga test drives.
type activationModules struct {
	Subscription subscription.Service
	Billing      billing.Service
}

// newActivationModules wires the real subscription and billing modules over
// the in-memory store below, so the saga tests exercise the facade path
// production uses.
func newActivationModules(store *activationStore, singleModel bool) activationModules {
	subMod := subscription.New(subscription.Deps{
		Plans:       store.subscribes,
		UserSubs:    store.users,
		Orders:      store.orders,
		Operations:  store,
		SingleModel: func() bool { return singleModel },
	})
	bilMod := billing.New(billing.Deps{
		PaidOrders:   billing.PaidOrderDependencies{Subscriptions: subMod},
		Orders:       store.orders,
		Store:        store,
		Tx:           store,
		UserProfiles: store.users,
		InvitePolicy: func() (uint8, bool) { return 0, false },
		SingleModel:  func() bool { return false },
		CurrencyUnit: func() string { return "CNY" },
	})
	return activationModules{Subscription: subMod, Billing: bilMod}
}

// errNotInScenario answers the repository calls no activation scenario
// makes. The billing and subscription facades take the complete repository
// contracts, so the fakes implement every method; the ones outside the
// scenarios are collected at the end of the file.
var errNotInScenario = errors.New("not part of the activation scenarios")

// activationStore is the persistence of one activation scenario. It serves
// the facades' store and transaction ports, and the billing and subscription
// transactions run their closures directly on it.
type activationStore struct {
	periods    activationPeriodRepo
	wallet     *activationWalletRepo
	orders     *activationOrderRepo
	users      *activationUserRepo
	subscribes *activationSubscribeRepo
	logs       *activationLogRepo
	inbox      *activationInboxRepo
}

var (
	_ billing.Store                = (*activationStore)(nil)
	_ billing.Transactor           = (*activationStore)(nil)
	_ subscription.Store           = (*activationStore)(nil)
	_ repository.BillingStore      = (*activationStore)(nil)
	_ repository.SubscriptionStore = (*activationStore)(nil)
)

func (s *activationStore) InBillingTx(_ context.Context, fn func(repository.BillingStore) error) error {
	return fn(s)
}

func (s *activationStore) InSubscriptionTx(_ context.Context, fn func(repository.SubscriptionStore) error) error {
	return fn(s)
}

// InPlatformTx serves the subscription module's quota tasks, which no
// activation scenario runs.
func (s *activationStore) InPlatformTx(context.Context, func(repository.PlatformStore) error) error {
	return errNotInScenario
}

func (s *activationStore) Entitlement() repository.EntitlementRepo { return &s.periods }
func (s *activationStore) Wallet() repository.WalletRepo           { return s.walletRepo() }
func (s *activationStore) Order() repository.OrderRepo             { return s.orders }
func (s *activationStore) UserSubscription() repository.UserSubscriptionRepo {
	return s.users
}
func (s *activationStore) Log() repository.LogRepo             { return s.logs }
func (s *activationStore) Subscribe() repository.SubscribeRepo { return s.subscribes }
func (s *activationStore) Inbox() repository.InboxRepo         { return s.inbox }

// The activation scenarios read or write no order events, payments, coupons,
// withdrawals, outbox events, traffic resets or task rows; these accessors
// only complete the store views the facades expect.
func (s *activationStore) OrderEvent() repository.OrderEventRepo         { return nil }
func (s *activationStore) Payment() repository.PaymentRepo               { return nil }
func (s *activationStore) Coupon() repository.CouponRepo                 { return nil }
func (s *activationStore) UserWithdrawal() repository.UserWithdrawalRepo { return nil }
func (s *activationStore) Outbox() repository.OutboxRepo                 { return nil }
func (s *activationStore) SubscriptionTraffic() repository.SubscriptionTrafficRepo {
	return nil
}
func (s *activationStore) Task() repository.TaskRepo { return nil }

func (s *activationStore) walletRepo() *activationWalletRepo {
	if s.wallet == nil {
		s.wallet = &activationWalletRepo{}
	}
	return s.wallet
}

// activationPeriodRepo keeps the entitlement periods the fulfillment
// records.
type activationPeriodRepo struct {
	rows map[string]*entitlement.Period
}

var _ repository.EntitlementRepo = (*activationPeriodRepo)(nil)

func (r *activationPeriodRepo) FindPeriod(_ context.Context, id string) (*entitlement.Period, error) {
	return r.rows[id], nil
}

func (r *activationPeriodRepo) InsertPeriod(_ context.Context, p *entitlement.Period) error {
	if r.rows == nil {
		r.rows = make(map[string]*entitlement.Period)
	}
	if r.rows[p.ID] != nil {
		return fmt.Errorf("duplicate period")
	}
	r.rows[p.ID] = p
	return nil
}

// activationInboxRepo keeps the inbox markers the activation stages record
// to skip themselves on a replay.
type activationInboxRepo struct {
	records map[string]*inboxEntity.Record
}

var _ repository.InboxRepo = (*activationInboxRepo)(nil)

func newActivationInboxRepo() *activationInboxRepo {
	return &activationInboxRepo{records: map[string]*inboxEntity.Record{}}
}

func (r *activationInboxRepo) Find(_ context.Context, consumer, eventKey string) (*inboxEntity.Record, error) {
	record, ok := r.records[consumer+"|"+eventKey]
	if !ok {
		return nil, nil
	}
	found := *record
	return &found, nil
}

func (r *activationInboxRepo) Insert(_ context.Context, consumer, eventKey, result string) error {
	key := consumer + "|" + eventKey
	if _, ok := r.records[key]; ok {
		return fmt.Errorf("duplicate inbox record %s", key)
	}
	r.records[key] = &inboxEntity.Record{Consumer: consumer, EventKey: eventKey, Result: result}
	return nil
}

// activationOrderRepo holds the scenario's one order. finalizeFailures fails
// that many settlement writes (the Paid to Finished transition) first.
type activationOrderRepo struct {
	order            *orderEntity.Order
	finalizeFailures int
}

var _ repository.OrderRepo = (*activationOrderRepo)(nil)

func (r *activationOrderRepo) FindOneByOrderNo(_ context.Context, orderNo string) (*orderEntity.Order, error) {
	if r.order.OrderNo != orderNo {
		return nil, gorm.ErrRecordNotFound
	}
	found := *r.order
	return &found, nil
}

func (r *activationOrderRepo) FindOneByOrderNoForUpdate(ctx context.Context, orderNo string) (*orderEntity.Order, error) {
	return r.FindOneByOrderNo(ctx, orderNo)
}

func (r *activationOrderRepo) SetCommission(_ context.Context, orderNo string, amount, refererID int64) error {
	if r.order.OrderNo == orderNo {
		r.order.Commission = amount
		r.order.CommissionRefererId = refererID
	}
	return nil
}

func (r *activationOrderRepo) HasCommissionedOrder(context.Context, int64, string) (bool, error) {
	return false, nil
}

func (r *activationOrderRepo) UpdateOrderStatusFrom(_ context.Context, orderNo string, from, to uint8) (bool, error) {
	if to == OrderStatusFinished && r.finalizeFailures > 0 {
		r.finalizeFailures--
		return false, errors.New("finalize write unavailable")
	}
	if r.order.OrderNo != orderNo || r.order.Status != from {
		return false, nil
	}
	r.order.Status = to
	return true, nil
}

// activationWalletRepo holds one wallet, the buyer's or the referrer's; a
// locked read of a user without a wallet opens one, as the repository does.
type activationWalletRepo struct {
	wallet *walletEntity.Wallet
}

var _ repository.WalletRepo = (*activationWalletRepo)(nil)

func (r *activationWalletRepo) FindWallet(_ context.Context, userId int64) (*walletEntity.Wallet, error) {
	if r.wallet == nil || r.wallet.UserId != userId {
		return nil, nil
	}
	found := *r.wallet
	return &found, nil
}

func (r *activationWalletRepo) FindOneForUpdate(_ context.Context, id int64) (*walletEntity.Wallet, error) {
	if r.wallet == nil {
		r.wallet = &walletEntity.Wallet{UserId: id}
	}
	if r.wallet.UserId != id {
		return nil, gorm.ErrRecordNotFound
	}
	found := *r.wallet
	return &found, nil
}

func (r *activationWalletRepo) UpdateBalanceFields(_ context.Context, data *walletEntity.Wallet) error {
	r.wallet.Balance = data.Balance
	r.wallet.GiftAmount = data.GiftAmount
	return nil
}

func (r *activationWalletRepo) UpdateCommission(_ context.Context, data *walletEntity.Wallet) error {
	r.wallet.Commission = data.Commission
	return nil
}

// activationUserRepo is the buyer's side of the scenario: the account and
// referrer profiles billing reads, and the user subscription the fulfillment
// creates or extends. quotaCount and blocking steer the quota and
// single-subscription checks, whose calls it counts.
type activationUserRepo struct {
	user             *userEntity.User
	profiles         map[int64]*userEntity.User
	quotaCount       int64
	quotaCountCalls  int
	blocking         bool
	hasBlockingCalls int
	subscription     *usersub.Subscribe
}

var _ repository.UserSubscriptionRepo = (*activationUserRepo)(nil)

// FindOne serves billing's profile reads of the buyer and the referrer.
func (r *activationUserRepo) FindOne(_ context.Context, id int64) (*userEntity.User, error) {
	if profile := r.profiles[id]; profile != nil {
		found := *profile
		return &found, nil
	}
	if r.user == nil || r.user.Id != id {
		return nil, gorm.ErrRecordNotFound
	}
	found := *r.user
	return &found, nil
}

func (r *activationUserRepo) LockUserSerial(_ context.Context, _ int64) error {
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

func (r *activationUserRepo) FindOneSubscribeByToken(_ context.Context, token string) (*usersub.Subscribe, error) {
	if r.subscription == nil || r.subscription.Token != token {
		return nil, gorm.ErrRecordNotFound
	}
	found := *r.subscription
	return &found, nil
}

func (r *activationUserRepo) FindOneSubscribeByTokenForUpdate(ctx context.Context, token string) (*usersub.Subscribe, error) {
	return r.FindOneSubscribeByToken(ctx, token)
}

func (r *activationUserRepo) UpdateSubscribeColumns(_ context.Context, data *usersub.Subscribe, _ ...string) error {
	updated := *data
	r.subscription = &updated
	return nil
}

func (r *activationUserRepo) ClearSubscribeCache(_ context.Context, _ ...*usersub.Subscribe) error {
	return nil
}

// activationLogRepo collects the audit entries the stages write.
type activationLogRepo struct {
	logs []*logEntity.SystemLog
}

var _ repository.LogRepo = (*activationLogRepo)(nil)

func (r *activationLogRepo) Insert(_ context.Context, data *logEntity.SystemLog) error {
	r.logs = append(r.logs, data)
	return nil
}

// activationSubscribeRepo holds the plan the order buys.
type activationSubscribeRepo struct {
	subscribe *subscribeEntity.Subscribe
}

var _ repository.SubscribeRepo = (*activationSubscribeRepo)(nil)

func (r *activationSubscribeRepo) FindOne(_ context.Context, id int64) (*subscribeEntity.Subscribe, error) {
	if r.subscribe == nil || r.subscribe.Id != id {
		return nil, gorm.ErrRecordNotFound
	}
	found := *r.subscribe
	return &found, nil
}

func (r *activationSubscribeRepo) ClearCache(_ context.Context, _ ...int64) error {
	return nil
}

// recordingActivator records the orders the handler asks billing to
// activate.
type recordingActivator struct {
	orders []string
	err    error
}

var _ PaidOrderActivator = (*recordingActivator)(nil)

func (a *recordingActivator) ActivatePaidOrder(_ context.Context, orderNo string) error {
	a.orders = append(a.orders, orderNo)
	return a.err
}

// The handler hands billing the order number the task carries and returns
// billing's failure for asynq to retry; a payload it cannot decode never
// reaches billing.
func TestActivateOrderHandlerHandsTheOrderToBilling(t *testing.T) {
	activator := &recordingActivator{}
	handler := NewActivateOrderHandler(activator)
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: "handler-order"})
	if err != nil {
		t.Fatal(err)
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)
	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("activation = %v", err)
	}
	activator.err = errors.New("billing unavailable")
	if err := handler.ProcessTask(context.Background(), task); !errors.Is(err, activator.err) {
		t.Fatalf("failed activation = %v, want billing's error", err)
	}
	if len(activator.orders) != 2 || activator.orders[0] != "handler-order" {
		t.Fatalf("activated orders = %v, want handler-order twice", activator.orders)
	}
	if err := handler.ProcessTask(context.Background(), asynq.NewTask(taskqueue.ForthwithActivateOrder, []byte("{"))); err == nil {
		t.Fatal("an undecodable payload was accepted")
	}
	if len(activator.orders) != 2 {
		t.Fatalf("an undecodable payload reached billing: %v", activator.orders)
	}
}

func TestCommissionIsNotCreditedAgainAfterFinalizeFailure(t *testing.T) {
	expire := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	store := &activationStore{
		orders: &activationOrderRepo{finalizeFailures: 1, order: &orderEntity.Order{
			OrderNo: "commission-retry", UserId: 7, Type: OrderTypeRenewal, Status: OrderStatusPaid,
			SubscribeId: 9, SubscribeToken: "renewal-token", Quantity: 1, Amount: 10000, FeeAmount: 100,
		}},
		wallet: &activationWalletRepo{wallet: &walletEntity.Wallet{UserId: 99}},
		users: &activationUserRepo{
			user:         &userEntity.User{Id: 7, RefererId: 99},
			profiles:     map[int64]*userEntity.User{99: {Id: 99, ReferralPercentage: 20}},
			subscription: &usersub.Subscribe{Id: 11, UserId: 7, SubscribeId: 9, Token: "renewal-token", ExpireTime: expire, Status: usersub.SubscribeStatusActive},
		},
		subscribes: &activationSubscribeRepo{subscribe: &subscribeEntity.Subscribe{Id: 9, UnitTime: "Month"}},
		logs:       &activationLogRepo{}, inbox: newActivationInboxRepo(),
	}
	handler := NewActivateOrderHandler(newActivationModules(store, false).Billing)
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: "commission-retry"})
	if err != nil {
		t.Fatal(err)
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)
	if err := handler.ProcessTask(context.Background(), task); err == nil {
		t.Fatal("expected finalize failure")
	}
	if store.wallet.wallet.Commission != 1980 || store.orders.order.Status != OrderStatusPaid {
		t.Fatal("commission must commit before a retryable finalize failure")
	}
	extendedOnce := store.users.subscription.ExpireTime
	for range 2 {
		if err := handler.ProcessTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	if store.wallet.wallet.Commission != 1980 || !store.users.subscription.ExpireTime.Equal(extendedOnce) || store.orders.order.Status != OrderStatusFinished {
		t.Fatal("replay duplicated commission or subscription fulfillment")
	}
	commissionLogs := 0
	for _, entry := range store.logs.logs {
		if entry.Type == logEntity.TypeCommission.Uint8() {
			commissionLogs++
		}
	}
	if commissionLogs != 1 {
		t.Fatalf("commission logs = %d, want 1", commissionLogs)
	}
}

func TestActivateRechargeCommitsSettlementOnlyOnce(t *testing.T) {
	store := &activationStore{
		orders: &activationOrderRepo{order: &orderEntity.Order{
			OrderNo: "recharge-order", UserId: 7, Type: OrderTypeRecharge, Price: 1250, Status: OrderStatusPaid,
		}},
		users:  &activationUserRepo{user: &userEntity.User{Id: 7}},
		wallet: &activationWalletRepo{wallet: &walletEntity.Wallet{UserId: 7, Balance: 500}},
		logs:   &activationLogRepo{},
		inbox:  newActivationInboxRepo(),
	}
	handler := NewActivateOrderHandler(newActivationModules(store, false).Billing)
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: "recharge-order"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)

	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("duplicate activation: %v", err)
	}
	if store.orders.order.Status != OrderStatusFinished {
		t.Fatalf("order status = %d, want finished", store.orders.order.Status)
	}
	if store.wallet.wallet.Balance != 1750 {
		t.Fatalf("balance = %d, want 1750", store.wallet.wallet.Balance)
	}
	if len(store.logs.logs) != 1 {
		t.Fatalf("recharge logs = %d, want 1", len(store.logs.logs))
	}
}

// TestActivateRechargeReplayAfterFulfillmentSkipsSecondCredit simulates a
// crash between the fulfillment and finalize stages: the balance credit
// committed but the order is still Paid, so the reconciler replays the task.
// The inbox marker must prevent a second credit.
func TestActivateRechargeReplayAfterFulfillmentSkipsSecondCredit(t *testing.T) {
	store := &activationStore{
		orders: &activationOrderRepo{order: &orderEntity.Order{
			OrderNo: "recharge-replay", UserId: 7, Type: OrderTypeRecharge, Price: 1250, Status: OrderStatusPaid,
		}},
		users:  &activationUserRepo{user: &userEntity.User{Id: 7}},
		wallet: &activationWalletRepo{wallet: &walletEntity.Wallet{UserId: 7, Balance: 500}},
		logs:   &activationLogRepo{},
		inbox:  newActivationInboxRepo(),
	}
	handler := NewActivateOrderHandler(newActivationModules(store, false).Billing)
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: "recharge-replay"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)

	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	// Simulate the finalize stage having been lost: the order is Paid again.
	store.orders.order.Status = OrderStatusPaid

	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("replayed activation: %v", err)
	}
	if store.wallet.wallet.Balance != 1750 {
		t.Fatalf("balance = %d, want 1750 (credited exactly once)", store.wallet.wallet.Balance)
	}
	if len(store.logs.logs) != 1 {
		t.Fatalf("balance logs = %d, want 1", len(store.logs.logs))
	}
	if store.orders.order.Status != OrderStatusFinished {
		t.Fatalf("order status = %d, want finished after replay", store.orders.order.Status)
	}
}

// TestActivateRenewalReplayExtendsSubscriptionOnce guards the most dangerous
// replay: extending a renewal twice would silently gift subscription time.
func TestActivateRenewalReplayExtendsSubscriptionOnce(t *testing.T) {
	expire := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	store := &activationStore{
		orders: &activationOrderRepo{order: &orderEntity.Order{
			OrderNo: "renewal-replay", UserId: 7, Type: OrderTypeRenewal, Status: OrderStatusPaid,
			SubscribeId: 9, SubscribeToken: "renewal-token", Quantity: 1,
		}},
		users: &activationUserRepo{
			user: &userEntity.User{Id: 7},
			subscription: &usersub.Subscribe{
				Id: 11, UserId: 7, SubscribeId: 9, Token: "renewal-token",
				ExpireTime: expire, Status: usersub.SubscribeStatusActive,
			},
		},
		subscribes: &activationSubscribeRepo{subscribe: &subscribeEntity.Subscribe{Id: 9, UnitTime: "Month"}},
		logs:       &activationLogRepo{},
		inbox:      newActivationInboxRepo(),
	}
	handler := NewActivateOrderHandler(newActivationModules(store, false).Billing)
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: "renewal-replay"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)

	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	extendedOnce := store.users.subscription.ExpireTime
	if !extendedOnce.After(expire) {
		t.Fatalf("first activation must extend the subscription: %v -> %v", expire, extendedOnce)
	}

	// Simulate the finalize stage having been lost: the order is Paid again.
	store.orders.order.Status = OrderStatusPaid

	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("replayed activation: %v", err)
	}
	if !store.users.subscription.ExpireTime.Equal(extendedOnce) {
		t.Fatalf("replay extended the subscription twice: %v -> %v", extendedOnce, store.users.subscription.ExpireTime)
	}
	if store.orders.order.Status != OrderStatusFinished {
		t.Fatalf("order status = %d, want finished after replay", store.orders.order.Status)
	}
}

func TestFulfillmentEnforcesQuota(t *testing.T) {
	users := &activationUserRepo{quotaCount: 1, user: &userEntity.User{Id: 7}}
	store := &activationStore{
		users:      users,
		orders:     &activationOrderRepo{order: &orderEntity.Order{OrderNo: "quota-order", UserId: 7, SubscribeId: 9, Type: OrderTypeSubscribe, Status: OrderStatusPaid}},
		subscribes: &activationSubscribeRepo{subscribe: &subscribeEntity.Subscribe{Id: 9, Quota: 1}},
		inbox:      newActivationInboxRepo(),
	}
	modules := newActivationModules(store, false)

	_, err := modules.Subscription.FulfillPaidOrder(context.Background(), "quota-order")
	if err == nil {
		t.Fatal("activation created a subscription after quota was exhausted")
	}
	if users.quotaCountCalls != 1 {
		t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
	}
}

func TestFulfillmentEnforcesSingleModel(t *testing.T) {
	users := &activationUserRepo{blocking: true, user: &userEntity.User{Id: 7}}
	store := &activationStore{
		users:      users,
		orders:     &activationOrderRepo{order: &orderEntity.Order{OrderNo: "single-order", UserId: 7, SubscribeId: 9, Type: OrderTypeSubscribe, Status: OrderStatusPaid}},
		subscribes: &activationSubscribeRepo{subscribe: &subscribeEntity.Subscribe{Id: 9}},
		inbox:      newActivationInboxRepo(),
	}
	modules := newActivationModules(store, true)

	_, err := modules.Subscription.FulfillPaidOrder(context.Background(), "single-order")
	if err == nil {
		t.Fatal("activation created a subscription despite a blocking subscription")
	}
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}

func TestFulfillResetTrafficClearsFinishedAt(t *testing.T) {
	finishedAt := time.Now().Add(-time.Hour)
	store := &activationStore{
		users: &activationUserRepo{
			user: &userEntity.User{Id: 7},
			subscription: &usersub.Subscribe{
				Id: 11, UserId: 7, SubscribeId: 9, Token: "subscription-token",
				Download: 100, Upload: 200, Status: usersub.SubscribeStatusFinished, FinishedAt: &finishedAt,
			},
		},
		orders:     &activationOrderRepo{order: &orderEntity.Order{OrderNo: "reset-order", UserId: 7, SubscribeToken: "subscription-token", Type: OrderTypeResetTraffic, Status: OrderStatusPaid}},
		subscribes: &activationSubscribeRepo{subscribe: &subscribeEntity.Subscribe{Id: 9}},
		logs:       &activationLogRepo{},
		inbox:      newActivationInboxRepo(),
	}
	modules := newActivationModules(store, false)

	if _, err := modules.Subscription.FulfillPaidOrder(context.Background(), "reset-order"); err != nil {
		t.Fatalf("activate reset traffic: %v", err)
	}
	if store.users.subscription.FinishedAt != nil {
		t.Fatal("reset traffic left FinishedAt set")
	}
	if store.users.subscription.Status != usersub.SubscribeStatusActive {
		t.Fatalf("status = %d, want active", store.users.subscription.Status)
	}
}

// The repository methods below are outside every activation scenario: the
// fakes implement them only to satisfy the contracts the facades take, and
// each answers errNotInScenario.

// The rest of repository.OrderRepo.
func (*activationOrderRepo) CountPendingByPaymentID(context.Context, int64) (int64, error) {
	return 0, errNotInScenario
}
func (*activationOrderRepo) CountPendingGuestOrders(context.Context, string, string, time.Time) (int64, error) {
	return 0, errNotInScenario
}
func (*activationOrderRepo) CountUserCouponUsage(context.Context, int64, string) (int64, error) {
	return 0, errNotInScenario
}
func (*activationOrderRepo) Delete(context.Context, int64) error { return errNotInScenario }
func (*activationOrderRepo) FindOne(context.Context, int64) (*orderEntity.Order, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) FindOneByIdempotencyKey(context.Context, string) (*orderEntity.Order, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) FindOneDetails(context.Context, int64) (*orderEntity.Details, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) FindOneDetailsByOrderNo(context.Context, string) (*orderEntity.Details, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) Insert(context.Context, *orderEntity.Order) error {
	return errNotInScenario
}
func (*activationOrderRepo) IsUserEligibleForNewOrder(context.Context, int64) (bool, error) {
	return false, errNotInScenario
}
func (*activationOrderRepo) MarkOrderPaid(context.Context, string, string) (bool, error) {
	return false, errNotInScenario
}
func (*activationOrderRepo) QueryDailyOrdersList(context.Context, time.Time) ([]orderEntity.OrdersTotalWithDate, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) QueryDailyReport(context.Context, time.Time) (*orderEntity.DailyReport, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) QueryDateOrders(context.Context, time.Time) (orderEntity.OrdersTotal, error) {
	return orderEntity.OrdersTotal{}, errNotInScenario
}
func (*activationOrderRepo) QueryDateUserCounts(context.Context, time.Time) (int64, int64, error) {
	return 0, 0, errNotInScenario
}
func (*activationOrderRepo) QueryMonthlyOrders(context.Context, time.Time) (orderEntity.OrdersTotal, error) {
	return orderEntity.OrdersTotal{}, errNotInScenario
}
func (*activationOrderRepo) QueryMonthlyOrdersList(context.Context, time.Time) ([]orderEntity.OrdersTotalWithDate, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) QueryMonthlyUserCounts(context.Context, time.Time) (int64, int64, error) {
	return 0, 0, errNotInScenario
}
func (*activationOrderRepo) QueryOrderListByPage(context.Context, int, int, uint8, int64, int64, string) (int64, []*orderEntity.Details, error) {
	return 0, nil, errNotInScenario
}
func (*activationOrderRepo) QueryOrdersByStatusAfterID(context.Context, uint8, int64, int) ([]*orderEntity.Order, error) {
	return nil, errNotInScenario
}
func (*activationOrderRepo) QueryTotalOrders(context.Context) (orderEntity.OrdersTotal, error) {
	return orderEntity.OrdersTotal{}, errNotInScenario
}
func (*activationOrderRepo) QueryTotalUserCounts(context.Context) (int64, int64, error) {
	return 0, 0, errNotInScenario
}
func (*activationOrderRepo) SetPaymentTradeNoIfEmpty(context.Context, string, string) (bool, error) {
	return false, errNotInScenario
}
func (*activationOrderRepo) ReservePaymentCapture(context.Context, string, string) (bool, error) {
	panic("unexpected payment capture")
}

func (*activationOrderRepo) BeginPaymentSession(context.Context, string) (bool, error) {
	return false, errNotInScenario
}
func (*activationOrderRepo) CompletePaymentSession(context.Context, string, string, string) (bool, error) {
	return false, errNotInScenario
}
func (*activationOrderRepo) Update(context.Context, *orderEntity.Order) error {
	return errNotInScenario
}
func (*activationOrderRepo) UpdatePaymentExpectation(context.Context, string, int64, string) (bool, error) {
	return false, errNotInScenario
}

// The rest of repository.EntitlementRepo.
func (*activationPeriodRepo) FindStateForUpdate(context.Context, string) (*entitlement.State, error) {
	return nil, errNotInScenario
}
func (*activationPeriodRepo) InsertRevision(context.Context, *entitlement.Revision) error {
	return errNotInScenario
}
func (*activationPeriodRepo) InsertState(context.Context, *entitlement.State) error {
	return errNotInScenario
}
func (*activationPeriodRepo) PlanIDs(context.Context, string) ([]int64, error) {
	return nil, errNotInScenario
}
func (*activationPeriodRepo) UpdatePeriod(context.Context, *entitlement.Period) error {
	return errNotInScenario
}
func (*activationPeriodRepo) UpdateState(context.Context, *entitlement.State) error {
	return errNotInScenario
}

// The rest of repository.InboxRepo.
func (*activationInboxRepo) DeleteProcessedBefore(context.Context, time.Time) (int64, error) {
	return 0, errNotInScenario
}

// The rest of repository.WalletRepo.
func (*activationWalletRepo) FindWalletsByUserIds(context.Context, []int64) (map[int64]*walletEntity.Wallet, error) {
	return nil, errNotInScenario
}

// The rest of repository.UserSubscriptionRepo.
func (*activationUserRepo) ApplyEntitlementProjection(context.Context, *usersub.Subscribe) error {
	return errNotInScenario
}
func (*activationUserRepo) BatchUpdateUserSubscribeWithTraffic(context.Context, []trafficEntity.SubscribeTrafficDelta) error {
	return errNotInScenario
}
func (*activationUserRepo) CountSubscribesByFilter(context.Context, *usersub.SubscribeFilter) (int64, error) {
	return 0, errNotInScenario
}
func (*activationUserRepo) CountUserSubscribesBySubscribeIdAndStatus(context.Context, int64, ...int64) (int64, error) {
	return 0, errNotInScenario
}
func (*activationUserRepo) DeleteSubscribeById(context.Context, int64) error { return errNotInScenario }
func (*activationUserRepo) FindExpiredSubscribes(context.Context, time.Time) ([]*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindExpiringSubscribes(context.Context, time.Time, time.Time) ([]*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindOneSubscribe(context.Context, int64) (*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindOneSubscribeByOrderId(context.Context, int64) (*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindOneSubscribeDetailsById(context.Context, int64) (*usersub.SubscribeDetails, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindOneSubscribeForUpdate(context.Context, int64) (*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindOneUserSubscribe(context.Context, int64) (*usersub.SubscribeDetails, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindSubscribeDetailsByIds(context.Context, []int64) ([]*usersub.SubscribeDetails, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindSubscribeDetailsByUserIds(context.Context, []int64) ([]*usersub.SubscribeDetails, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindSubscribesByIds(context.Context, []int64) ([]*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindTrafficExceededSubscribes(context.Context) ([]*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindUserSubscribesByStatus(context.Context, ...int64) ([]*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) FindUsersSubscribeBySubscribeIds(context.Context, []int64) ([]*usersub.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) InsertSubscribe(context.Context, *usersub.Subscribe) error {
	return errNotInScenario
}
func (*activationUserRepo) MarkSubscribesFinished(context.Context, []int64, uint8, time.Time) error {
	return errNotInScenario
}
func (*activationUserRepo) QueryActiveSubscriptions(context.Context, ...int64) (map[int64]int64, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) QuerySubscribeIdsByFilter(context.Context, *usersub.SubscribeFilter) ([]int64, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) QueryUserSubscribe(context.Context, int64, ...int64) ([]*usersub.SubscribeDetails, error) {
	return nil, errNotInScenario
}
func (*activationUserRepo) RotateSubscribeCredentials(context.Context, []repository.SubscriptionCredentialRotation) error {
	return errNotInScenario
}

// The rest of repository.LogRepo.
func (*activationLogRepo) Delete(context.Context, int64) error           { return errNotInScenario }
func (*activationLogRepo) DeleteBefore(context.Context, time.Time) error { return errNotInScenario }
func (*activationLogRepo) DeleteBeforeBatch(context.Context, time.Time, int) (int64, error) {
	return 0, errNotInScenario
}
func (*activationLogRepo) FilterSystemLog(context.Context, *logEntity.FilterParams) ([]*logEntity.SystemLog, int64, error) {
	return nil, 0, errNotInScenario
}
func (*activationLogRepo) FindByDatesType(context.Context, []string, uint8) ([]*logEntity.SystemLog, error) {
	return nil, errNotInScenario
}
func (*activationLogRepo) FindFirstByDateType(context.Context, string, uint8) (*logEntity.SystemLog, error) {
	return nil, errNotInScenario
}
func (*activationLogRepo) FindOne(context.Context, int64) (*logEntity.SystemLog, error) {
	return nil, errNotInScenario
}
func (*activationLogRepo) InsertBatch(context.Context, []*logEntity.SystemLog, int) error {
	return errNotInScenario
}
func (*activationLogRepo) SumAmountByTypeAndObjectID(context.Context, uint8, int64) (int64, error) {
	return 0, errNotInScenario
}
func (*activationLogRepo) Update(context.Context, *logEntity.SystemLog) error {
	return errNotInScenario
}

// The rest of repository.SubscribeRepo.
func (*activationSubscribeRepo) BatchDeleteGroup(context.Context, []int64) error {
	return errNotInScenario
}
func (*activationSubscribeRepo) CreateGroup(context.Context, *subscribeEntity.Group) error {
	return errNotInScenario
}
func (*activationSubscribeRepo) Delete(context.Context, int64) error      { return errNotInScenario }
func (*activationSubscribeRepo) DeleteGroup(context.Context, int64) error { return errNotInScenario }
func (*activationSubscribeRepo) FilterList(context.Context, *subscribeEntity.FilterParams) (int64, []*subscribeEntity.Subscribe, error) {
	return 0, nil, errNotInScenario
}
func (*activationSubscribeRepo) FindByNodeScope(context.Context, []int64, []string) ([]*subscribeEntity.Subscribe, error) {
	return nil, errNotInScenario
}
func (*activationSubscribeRepo) Insert(context.Context, *subscribeEntity.Subscribe) error {
	return errNotInScenario
}
func (*activationSubscribeRepo) QueryGroupList(context.Context) (int64, []*subscribeEntity.Group, error) {
	return 0, nil, errNotInScenario
}
func (*activationSubscribeRepo) QueryResetCycleSubscribeIds(context.Context, int) ([]int64, error) {
	return nil, errNotInScenario
}
func (*activationSubscribeRepo) QuerySubscribeMinSortByIds(context.Context, []int64) (int64, error) {
	return 0, errNotInScenario
}
func (*activationSubscribeRepo) ReserveInventory(context.Context, int64) (repository.InventoryReservation, error) {
	return repository.InventoryUnavailable, errNotInScenario
}
func (*activationSubscribeRepo) RestoreInventory(context.Context, int64) error {
	return errNotInScenario
}
func (*activationSubscribeRepo) Update(context.Context, *subscribeEntity.Subscribe) error {
	return errNotInScenario
}
func (*activationSubscribeRepo) UpdateGroup(context.Context, *subscribeEntity.Group) error {
	return errNotInScenario
}
func (*activationSubscribeRepo) UpdateSort(context.Context, []*subscribeEntity.Subscribe) error {
	return errNotInScenario
}
