package checkout

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
)

type policyUserSubs struct {
	UserSubscriptionReader
	blocking           bool
	quotaCount         int64
	subscription       *userEntity.Subscribe
	hasBlockingCalls   int
	quotaCountCalls    int
	findSubscribeCalls int
}

func (r *policyUserSubs) HasBlockingSubscription(_ context.Context, _ int64) (bool, error) {
	r.hasBlockingCalls++
	return r.blocking, nil
}

func (r *policyUserSubs) CountQuotaConsumingSubscriptions(_ context.Context, _ int64, _ int64) (int64, error) {
	r.quotaCountCalls++
	return r.quotaCount, nil
}

func (r *policyUserSubs) FindOneSubscribe(_ context.Context, _ int64) (*userEntity.Subscribe, error) {
	r.findSubscribeCalls++
	return r.subscription, nil
}

type policyPlans struct {
	subscribe *subscribe.Subscribe
}

func (r policyPlans) FindOne(_ context.Context, _ int64) (*subscribe.Subscribe, error) {
	return r.subscribe, nil
}

func TestPurchaseSingleModelUsesBlockingSubscriptionPolicy(t *testing.T) {
	users := &policyUserSubs{blocking: true}
	svc := NewService(Deps{UserSubs: users, SingleModel: true})

	_, err := svc.Purchase(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
	if err == nil || !strings.Contains(err.Error(), "user has subscription") {
		t.Fatalf("Purchase error = %v, want single-model rejection", err)
	}
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}

func TestPurchaseAllowsDeductedSubscriptionPastSingleModelCheck(t *testing.T) {
	users := &policyUserSubs{blocking: false}
	svc := NewService(Deps{
		UserSubs:    users,
		Plans:       policyPlans{subscribe: &subscribe.Subscribe{Sell: boolPtr(true), Inventory: 0}},
		SingleModel: true,
	})

	_, err := svc.Purchase(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
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
		users := &policyUserSubs{quotaCount: 1}
		svc := NewService(Deps{UserSubs: users, Plans: policyPlans{subscribe: plan}})

		_, err := svc.Purchase(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
		if err == nil || !strings.Contains(err.Error(), "quota limit") {
			t.Fatalf("Purchase error = %v, want quota rejection", err)
		}
		if users.quotaCountCalls != 1 {
			t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
		}
	})

	t.Run("pre-create", func(t *testing.T) {
		users := &policyUserSubs{quotaCount: 1}
		svc := NewService(Deps{UserSubs: users, Plans: policyPlans{subscribe: plan}})

		_, err := svc.PreCreateOrder(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
		if err == nil || !strings.Contains(err.Error(), "quota limit") {
			t.Fatalf("PreCreateOrder error = %v, want quota rejection", err)
		}
		if users.quotaCountCalls != 1 {
			t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
		}
	})
}

func TestRenewalPreviewSkipsNewPurchaseQuotaAfterValidation(t *testing.T) {
	users := &policyUserSubs{
		quotaCount: 1,
		subscription: &userEntity.Subscribe{
			Id:          22,
			UserId:      42,
			SubscribeId: 10,
			Status:      userEntity.SubscribeStatusExpired,
		},
	}
	svc := NewService(Deps{
		UserSubs: users,
		Plans:    policyPlans{subscribe: &subscribe.Subscribe{Id: 10, Quota: 1, UnitPrice: 100}},
	})

	_, err := svc.PreCreateOrder(ownerContext(42), &dto.PurchaseOrderRequest{
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
			users := &policyUserSubs{
				quotaCount:   1,
				subscription: tt.subscription,
			}
			svc := NewService(Deps{
				UserSubs: users,
				Plans:    policyPlans{subscribe: &subscribe.Subscribe{Id: 10, Quota: 1}},
			})

			_, err := svc.PreCreateOrder(ownerContext(42), &dto.PurchaseOrderRequest{
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
