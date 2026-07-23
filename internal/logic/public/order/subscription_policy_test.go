package order

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
)

type subscriptionPolicyStore struct {
	repository.Store
	users      *subscriptionPolicyUserRepo
	subscribes *subscriptionPolicySubscribeRepo
}

func (s subscriptionPolicyStore) User() repository.UserRepo { return s.users }
func (s subscriptionPolicyStore) UserSubscription() repository.UserSubscriptionRepo {
	return s.users
}
func (s subscriptionPolicyStore) Subscribe() repository.SubscribeRepo { return s.subscribes }

type subscriptionPolicyUserRepo struct {
	repository.UserRepo
	repository.UserSubscriptionRepo
	blocking           bool
	quotaCount         int64
	subscription       *userEntity.Subscribe
	hasBlockingCalls   int
	quotaCountCalls    int
	findSubscribeCalls int
}

func (r *subscriptionPolicyUserRepo) HasBlockingSubscription(_ context.Context, _ int64) (bool, error) {
	r.hasBlockingCalls++
	return r.blocking, nil
}

func (r *subscriptionPolicyUserRepo) CountQuotaConsumingSubscriptions(_ context.Context, _ int64, _ int64) (int64, error) {
	r.quotaCountCalls++
	return r.quotaCount, nil
}

func (r *subscriptionPolicyUserRepo) FindOneSubscribe(_ context.Context, _ int64) (*userEntity.Subscribe, error) {
	r.findSubscribeCalls++
	return r.subscription, nil
}

type subscriptionPolicySubscribeRepo struct {
	repository.SubscribeRepo
	subscribe *subscribe.Subscribe
}

func (r *subscriptionPolicySubscribeRepo) FindOne(_ context.Context, _ int64) (*subscribe.Subscribe, error) {
	return r.subscribe, nil
}

func subscriptionPolicyContext() context.Context {
	return context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 42})
}

func TestPurchaseSingleModelUsesBlockingSubscriptionPolicy(t *testing.T) {
	users := &subscriptionPolicyUserRepo{blocking: true}
	logic := NewPurchaseLogic(subscriptionPolicyContext(), &svc.ServiceContext{
		Store:  subscriptionPolicyStore{users: users},
		Config: config.Config{Subscribe: config.SubscribeConfig{SingleModel: true}},
	})

	_, err := logic.Purchase(&dto.PurchaseOrderRequest{SubscribeId: 10})
	if err == nil || !strings.Contains(err.Error(), "user has subscription") {
		t.Fatalf("Purchase error = %v, want single-model rejection", err)
	}
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}

func TestPurchaseAllowsDeductedSubscriptionPastSingleModelCheck(t *testing.T) {
	users := &subscriptionPolicyUserRepo{blocking: false}
	logic := NewPurchaseLogic(subscriptionPolicyContext(), &svc.ServiceContext{
		Store: subscriptionPolicyStore{
			users: users,
			subscribes: &subscriptionPolicySubscribeRepo{subscribe: &subscribe.Subscribe{
				Sell:      boolPtr(true),
				Inventory: 0,
			}},
		},
		Config: config.Config{Subscribe: config.SubscribeConfig{SingleModel: true}},
	})

	_, err := logic.Purchase(&dto.PurchaseOrderRequest{SubscribeId: 10})
	if err == nil || strings.Contains(err.Error(), "user has subscription") {
		t.Fatalf("Purchase error = %v, want later validation after a non-blocking subscription", err)
	}
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}

func TestPurchaseAndPreCreateUseQuotaConsumingCount(t *testing.T) {
	plan := &subscribe.Subscribe{Sell: boolPtr(true), Inventory: -1, Quota: 1}

	t.Run("purchase", func(t *testing.T) {
		users := &subscriptionPolicyUserRepo{quotaCount: 1}
		logic := NewPurchaseLogic(subscriptionPolicyContext(), &svc.ServiceContext{Store: subscriptionPolicyStore{
			users:      users,
			subscribes: &subscriptionPolicySubscribeRepo{subscribe: plan},
		}})

		_, err := logic.Purchase(&dto.PurchaseOrderRequest{SubscribeId: 10})
		if err == nil || !strings.Contains(err.Error(), "quota limit") {
			t.Fatalf("Purchase error = %v, want quota rejection", err)
		}
		if users.quotaCountCalls != 1 {
			t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
		}
	})

	t.Run("pre-create", func(t *testing.T) {
		users := &subscriptionPolicyUserRepo{quotaCount: 1}
		logic := NewPreCreateOrderLogic(subscriptionPolicyContext(), &svc.ServiceContext{Store: subscriptionPolicyStore{
			users:      users,
			subscribes: &subscriptionPolicySubscribeRepo{subscribe: plan},
		}})

		_, err := logic.PreCreateOrder(&dto.PurchaseOrderRequest{SubscribeId: 10})
		if err == nil || !strings.Contains(err.Error(), "quota limit") {
			t.Fatalf("PreCreateOrder error = %v, want quota rejection", err)
		}
		if users.quotaCountCalls != 1 {
			t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
		}
	})
}

func TestRenewalPreviewSkipsNewPurchaseQuotaAfterValidation(t *testing.T) {
	users := &subscriptionPolicyUserRepo{
		quotaCount: 1,
		subscription: &userEntity.Subscribe{
			Id:          22,
			UserId:      42,
			SubscribeId: 10,
			Status:      userEntity.SubscribeStatusExpired,
		},
	}
	logic := NewPreCreateOrderLogic(subscriptionPolicyContext(), &svc.ServiceContext{Store: subscriptionPolicyStore{
		users: users,
		subscribes: &subscriptionPolicySubscribeRepo{subscribe: &subscribe.Subscribe{
			Id:        10,
			Quota:     1,
			UnitPrice: 100,
		}},
	}})

	_, err := logic.PreCreateOrder(&dto.PurchaseOrderRequest{
		SubscribeId:     10,
		UserSubscribeId: 22,
		Quantity:        1,
	})
	if err != nil {
		t.Fatalf("PreCreateOrder renewal preview error = %v, want nil", err)
	}
	if users.findSubscribeCalls != 1 {
		t.Fatalf("FindOneSubscribe calls = %d, want 1", users.findSubscribeCalls)
	}
	if users.quotaCountCalls != 0 {
		t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 0", users.quotaCountCalls)
	}
}

func TestRenewalPreviewValidatesTargetBeforeSkippingQuota(t *testing.T) {
	tests := []struct {
		name         string
		subscription *userEntity.Subscribe
		wantError    string
	}{
		{
			name: "foreign subscription",
			subscription: &userEntity.Subscribe{
				Id:          22,
				UserId:      7,
				SubscribeId: 10,
				Status:      userEntity.SubscribeStatusActive,
			},
			wantError: "does not belong to current user",
		},
		{
			name: "different plan",
			subscription: &userEntity.Subscribe{
				Id:          22,
				UserId:      42,
				SubscribeId: 11,
				Status:      userEntity.SubscribeStatusActive,
			},
			wantError: "does not match subscribe plan",
		},
		{
			name: "deducted subscription",
			subscription: &userEntity.Subscribe{
				Id:          22,
				UserId:      42,
				SubscribeId: 10,
				Status:      userEntity.SubscribeStatusDeducted,
			},
			wantError: "status does not allow renewal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &subscriptionPolicyUserRepo{
				quotaCount:   1,
				subscription: tt.subscription,
			}
			logic := NewPreCreateOrderLogic(subscriptionPolicyContext(), &svc.ServiceContext{Store: subscriptionPolicyStore{
				users: users,
				subscribes: &subscriptionPolicySubscribeRepo{subscribe: &subscribe.Subscribe{
					Id:    10,
					Quota: 1,
				}},
			}})

			_, err := logic.PreCreateOrder(&dto.PurchaseOrderRequest{
				SubscribeId:     10,
				UserSubscribeId: 22,
				Quantity:        1,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("PreCreateOrder error = %v, want %q", err, tt.wantError)
			}
			if users.quotaCountCalls != 0 {
				t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 0", users.quotaCountCalls)
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }
