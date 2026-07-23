package user

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

type adminSubscriptionPolicyStore struct {
	repository.Store
	users *adminSubscriptionPolicyUserRepo
}

func (s adminSubscriptionPolicyStore) User() repository.UserRepo { return s.users }
func (s adminSubscriptionPolicyStore) UserSubscription() repository.UserSubscriptionRepo {
	return s.users
}

type adminSubscriptionPolicyUserRepo struct {
	repository.UserRepo
	repository.UserSubscriptionRepo
	blocking         bool
	hasBlockingCalls int
}

func (r *adminSubscriptionPolicyUserRepo) FindOne(_ context.Context, _ int64) (*userEntity.User, error) {
	return &userEntity.User{Id: 7}, nil
}

func (r *adminSubscriptionPolicyUserRepo) HasBlockingSubscription(_ context.Context, _ int64) (bool, error) {
	r.hasBlockingCalls++
	return r.blocking, nil
}

func TestCreateUserSubscribeUsesBlockingSubscriptionPolicy(t *testing.T) {
	users := &adminSubscriptionPolicyUserRepo{blocking: true}
	logic := NewCreateUserSubscribeLogic(context.Background(), &svc.ServiceContext{
		Store:  adminSubscriptionPolicyStore{users: users},
		Config: config.Config{Subscribe: config.SubscribeConfig{SingleModel: true}},
	})

	err := logic.CreateUserSubscribe(&dto.CreateUserSubscribeRequest{UserId: 7})
	if err == nil || !strings.Contains(err.Error(), "Single subscribe mode exceeds limit") {
		t.Fatalf("CreateUserSubscribe error = %v, want single-model rejection", err)
	}
	if users.hasBlockingCalls != 1 {
		t.Fatalf("HasBlockingSubscription calls = %d, want 1", users.hasBlockingCalls)
	}
}
