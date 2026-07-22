package order

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
)

type ownershipStore struct {
	repository.Store
	users repository.UserRepo
}

func (s ownershipStore) User() repository.UserRepo { return s.users }

type ownershipUserRepo struct {
	repository.UserRepo
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
