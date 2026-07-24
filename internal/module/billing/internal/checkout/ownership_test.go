package checkout

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/constant"
)

type ownershipUserSubs struct {
	UserSubscriptionReader
	subscribe *userEntity.SubscribeDetails
}

func (r ownershipUserSubs) FindOneUserSubscribe(_ context.Context, _ int64) (*userEntity.SubscribeDetails, error) {
	return r.subscribe, nil
}

func ownerContext(id int64) context.Context {
	return context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: id})
}

func TestRenewalRejectsSubscriptionOwnedByAnotherUser(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &userEntity.SubscribeDetails{Id: 22, UserId: 33}}})

	_, err := svc.Renewal(ownerContext(11), &dto.RenewalOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("renewal accepted a subscription owned by another user")
	}
}

func TestRenewalRejectsDeductedSubscription(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &userEntity.SubscribeDetails{Id: 22, UserId: 11, Status: userEntity.SubscribeStatusDeducted}}})

	_, err := svc.Renewal(ownerContext(11), &dto.RenewalOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("renewal accepted a deducted subscription")
	}
}

func TestResetTrafficRejectsSubscriptionOwnedByAnotherUser(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &userEntity.SubscribeDetails{Id: 22, UserId: 33}}})

	_, err := svc.ResetTraffic(ownerContext(11), &dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("reset traffic accepted a subscription owned by another user")
	}
}

func TestResetTrafficRejectsExpiredSubscription(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &userEntity.SubscribeDetails{
		Id: 22, UserId: 11, ExpireTime: time.Now().Add(-time.Minute),
	}}})

	_, err := svc.ResetTraffic(ownerContext(11), &dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	if err == nil {
		t.Fatal("reset traffic accepted an expired subscription")
	}
}
