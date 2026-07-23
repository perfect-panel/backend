package order

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
)

type ownershipStore struct {
	repository.Store
	users ownershipUserRepo
}

func (s ownershipStore) User() repository.UserRepo { return s.users }
func (s ownershipStore) UserSubscription() repository.UserSubscriptionRepo {
	return s.users
}

type ownershipUserRepo struct {
	repository.UserRepo
	repository.UserSubscriptionRepo
	subscribe *userEntity.SubscribeDetails
}

func (r ownershipUserRepo) FindOneUserSubscribe(_ context.Context, _ int64) (*userEntity.SubscribeDetails, error) {
	return r.subscribe, nil
}

func TestRenewalRejectsSubscriptionOwnedByAnotherUser(t *testing.T) {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 11})
	logic := NewRenewalLogic(ctx, &svc.ServiceContext{Store: ownershipStore{
		users: ownershipUserRepo{subscribe: &userEntity.SubscribeDetails{Id: 22, UserId: 33}},
	}})

	_, err := logic.Renewal(&dto.RenewalOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("renewal accepted a subscription owned by another user")
	}
}

func TestRenewalRejectsDeductedSubscription(t *testing.T) {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 11})
	logic := NewRenewalLogic(ctx, &svc.ServiceContext{Store: ownershipStore{
		users: ownershipUserRepo{subscribe: &userEntity.SubscribeDetails{Id: 22, UserId: 11, Status: userEntity.SubscribeStatusDeducted}},
	}})

	_, err := logic.Renewal(&dto.RenewalOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("renewal accepted a deducted subscription")
	}
}

func TestResetTrafficRejectsSubscriptionOwnedByAnotherUser(t *testing.T) {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 11})
	logic := NewResetTrafficLogic(ctx, &svc.ServiceContext{Store: ownershipStore{
		users: ownershipUserRepo{subscribe: &userEntity.SubscribeDetails{Id: 22, UserId: 33}},
	}})

	_, err := logic.ResetTraffic(&dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("reset traffic accepted a subscription owned by another user")
	}
}

func TestResetTrafficRejectsExpiredSubscription(t *testing.T) {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 11})
	logic := NewResetTrafficLogic(ctx, &svc.ServiceContext{Store: ownershipStore{
		users: ownershipUserRepo{subscribe: &userEntity.SubscribeDetails{
			Id: 22, UserId: 11, ExpireTime: time.Now().Add(-time.Minute),
		}},
	}})

	_, err := logic.ResetTraffic(&dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("reset traffic accepted an expired subscription")
	}
}
