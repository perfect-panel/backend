package checkout

import (
	"context"
	"errors"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	userEntity "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ownershipUserSubs serves the one user subscription a renewal or traffic
// reset targets; those flows read nothing else before they refuse.
type ownershipUserSubs struct {
	subscribe *usersub.SubscribeDetails
}

var _ UserSubscriptionReader = ownershipUserSubs{}

var errOwnershipOnly = errors.New("ownershipUserSubs: only the targeted subscription is read")

func (r ownershipUserSubs) FindOneUserSubscribe(_ context.Context, _ int64) (*usersub.SubscribeDetails, error) {
	return r.subscribe, nil
}

func (ownershipUserSubs) HasBlockingSubscription(context.Context, int64) (bool, error) {
	return false, errOwnershipOnly
}

func (ownershipUserSubs) CountQuotaConsumingSubscriptions(context.Context, int64, int64) (int64, error) {
	return 0, errOwnershipOnly
}

func (ownershipUserSubs) FindOneSubscribe(context.Context, int64) (*usersub.Subscribe, error) {
	return nil, errOwnershipOnly
}

func ownerContext(id int64) context.Context {
	return userEntity.NewContext(context.Background(), &userEntity.User{Id: id})
}

func TestRenewalRejectsSubscriptionOwnedByAnotherUser(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &usersub.SubscribeDetails{Id: 22, UserId: 33}}})

	_, err := svc.Renewal(ownerContext(11), &dto.RenewalOrderRequest{UserSubscribeID: 22})
	assertCode(t, err, xerr.InvalidAccess)
}

func TestRenewalRejectsDeductedSubscription(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &usersub.SubscribeDetails{Id: 22, UserId: 11, Status: usersub.SubscribeStatusDeducted}}})

	_, err := svc.Renewal(ownerContext(11), &dto.RenewalOrderRequest{UserSubscribeID: 22})
	assertCode(t, err, xerr.SubscribeNotAvailable)
}

func TestResetTrafficRejectsSubscriptionOwnedByAnotherUser(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &usersub.SubscribeDetails{Id: 22, UserId: 33}}})

	_, err := svc.ResetTraffic(ownerContext(11), &dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	assertCode(t, err, xerr.InvalidAccess)
}

func TestResetTrafficRejectsExpiredSubscription(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &usersub.SubscribeDetails{
		Id: 22, UserId: 11, ExpireTime: time.Now().Add(-time.Minute),
	}}})

	_, err := svc.ResetTraffic(ownerContext(11), &dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	assertCode(t, err, xerr.SubscribeNotAvailable)
}

func TestLocalCheckoutRejectsProviderManagedSubscription(t *testing.T) {
	svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &usersub.SubscribeDetails{Id: 22, UserId: 11, EntitlementSource: "apple"}}})
	_, err := svc.Renewal(ownerContext(11), &dto.RenewalOrderRequest{UserSubscribeID: 22, Quantity: 1})
	if !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("local renewal accepted: %v", err)
	}
	_, err = svc.ResetTraffic(ownerContext(11), &dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
	if !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("local reset checkout accepted: %v", err)
	}
}
