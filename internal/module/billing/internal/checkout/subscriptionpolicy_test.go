package checkout

import (
	"context"
	"errors"
	"strings"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// policyUserSubs answers the single-subscription and quota policies and
// counts their reads; the policies never read a user subscription's details.
type policyUserSubs struct {
	blocking           bool
	quotaCount         int64
	subscription       *usersub.Subscribe
	hasBlockingCalls   int
	quotaCountCalls    int
	findSubscribeCalls int
}

var _ UserSubscriptionReader = (*policyUserSubs)(nil)

func (r *policyUserSubs) HasBlockingSubscription(_ context.Context, _ int64) (bool, error) {
	r.hasBlockingCalls++
	return r.blocking, nil
}

func (r *policyUserSubs) CountQuotaConsumingSubscriptions(_ context.Context, _ int64, _ int64) (int64, error) {
	r.quotaCountCalls++
	return r.quotaCount, nil
}

func (r *policyUserSubs) FindOneSubscribe(_ context.Context, _ int64) (*usersub.Subscribe, error) {
	r.findSubscribeCalls++
	return r.subscription, nil
}

func (*policyUserSubs) FindOneUserSubscribe(context.Context, int64) (*usersub.SubscribeDetails, error) {
	return nil, errors.New("policyUserSubs: the policies do not read subscription details")
}

type policyPlans struct {
	subscribe *subscribe.Subscribe
}

func (r policyPlans) FindOne(_ context.Context, _ int64) (*subscribe.Subscribe, error) {
	return r.subscribe, nil
}

func TestPurchaseSingleModelUsesBlockingSubscriptionPolicy(t *testing.T) {
	users := &policyUserSubs{blocking: true}
	svc := NewService(Deps{UserSubs: users, SingleModel: func() bool { return true }})

	_, err := svc.Purchase(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
	assertCode(t, err, xerr.UserSubscribeExist)
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}

func TestPurchaseAllowsDeductedSubscriptionPastSingleModelCheck(t *testing.T) {
	users := &policyUserSubs{blocking: false}
	svc := NewService(Deps{
		UserSubs:    users,
		Plans:       policyPlans{subscribe: &subscribe.Subscribe{Sell: boolPtr(true), Inventory: 0}},
		SingleModel: func() bool { return true },
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
		svc := NewService(Deps{UserSubs: users, Plans: policyPlans{subscribe: plan}, SingleModel: func() bool { return false }})

		_, err := svc.Purchase(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
		assertCode(t, err, xerr.SubscribeQuotaLimit)
		if users.quotaCountCalls != 1 {
			t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
		}
	})

	t.Run("pre-create", func(t *testing.T) {
		users := &policyUserSubs{quotaCount: 1}
		svc := NewService(Deps{UserSubs: users, Plans: policyPlans{subscribe: plan}, SingleModel: func() bool { return false }})

		_, err := svc.PreCreateOrder(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10})
		assertCode(t, err, xerr.SubscribeQuotaLimit)
		if users.quotaCountCalls != 1 {
			t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 1", users.quotaCountCalls)
		}
	})
}

func TestRenewalPreviewSkipsNewPurchaseQuotaAfterValidation(t *testing.T) {
	users := &policyUserSubs{
		quotaCount: 1,
		subscription: &usersub.Subscribe{
			Id:          22,
			UserId:      42,
			SubscribeId: 10,
			Status:      usersub.SubscribeStatusExpired,
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
		subscription *usersub.Subscribe
		wantError    string
		wantCode     uint32
	}{
		{
			name: "foreign subscription",
			subscription: &usersub.Subscribe{
				Id:          22,
				UserId:      7,
				SubscribeId: 10,
				Status:      usersub.SubscribeStatusActive,
			},
			wantError: "does not belong to current user",
			wantCode:  xerr.InvalidAccess,
		},
		{
			name: "different plan",
			subscription: &usersub.Subscribe{
				Id:          22,
				UserId:      42,
				SubscribeId: 11,
				Status:      usersub.SubscribeStatusActive,
			},
			wantError: "does not match subscribe plan",
			wantCode:  xerr.InvalidParams,
		},
		{
			name: "deducted subscription",
			subscription: &usersub.Subscribe{
				Id:          22,
				UserId:      42,
				SubscribeId: 10,
				Status:      usersub.SubscribeStatusDeducted,
			},
			wantError: "refunded or stopped subscription cannot be renewed",
			wantCode:  xerr.SubscribeNotAvailable,
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
			assertCode(t, err, tt.wantCode)
			if users.quotaCountCalls != 0 {
				t.Fatalf("CountQuotaConsumingSubscriptions calls = %d, want 0", users.quotaCountCalls)
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }
