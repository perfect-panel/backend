package adminuser

import (
	"context"
	"errors"
	"reflect"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// accessCaches stands in for the subscription and network facades: the
// subscription side reports a fixed node scope, and both sides record what
// they were asked to clear.
type accessCaches struct {
	nodeIDs []int64
	tags    []string
	subErr  error
	users   [][]int64
	scopes  []scopeCall
}

type scopeCall struct {
	nodeIDs []int64
	tags    []string
}

var (
	_ SubscriptionCaches = (*accessCaches)(nil)
	_ ServerCaches       = (*accessCaches)(nil)
)

func (c *accessCaches) ClearUserSubscriptionCaches(_ context.Context, userIDs []int64) ([]int64, []string, error) {
	c.users = append(c.users, userIDs)
	if c.subErr != nil {
		return nil, nil, c.subErr
	}
	return c.nodeIDs, c.tags, nil
}

func (c *accessCaches) ClearServerCachesByNodeScope(_ context.Context, nodeIDs []int64, tags []string) error {
	c.scopes = append(c.scopes, scopeCall{nodeIDs: nodeIDs, tags: tags})
	return nil
}

func newAccessFixture(t *testing.T, caches *accessCaches) (*identitytest.Env, *Service) {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	return env, NewService(Deps{Users: env.Store.User(), SubscriptionCaches: caches, ServerCaches: caches})
}

func createAccount(t *testing.T, env *identitytest.Env) *user.User {
	t.Helper()
	enabled := true
	u := &user.User{Enable: &enabled}
	if err := env.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// Deleting accounts asks the subscription module once for all of them and
// clears the node caches of their plans' whole scope in one call, however
// many subscriptions and plans the accounts hold.
func TestDeletingAccountsClearsTheirAccessCachesInOneBatch(t *testing.T) {
	caches := &accessCaches{nodeIDs: []int64{5, 6}, tags: []string{"hk"}}
	env, svc := newAccessFixture(t, caches)
	first, second := createAccount(t, env), createAccount(t, env)

	if err := svc.BatchDeleteUser(context.Background(), &dto.BatchDeleteUserRequest{Ids: []int64{first.Id, second.Id, first.Id}}); err != nil {
		t.Fatalf("BatchDeleteUser() error = %v", err)
	}
	if want := [][]int64{{first.Id, second.Id}}; !reflect.DeepEqual(caches.users, want) {
		t.Fatalf("subscription cache calls = %v, want %v", caches.users, want)
	}
	if want := []scopeCall{{nodeIDs: []int64{5, 6}, tags: []string{"hk"}}}; !reflect.DeepEqual(caches.scopes, want) {
		t.Fatalf("node cache calls = %+v, want %+v", caches.scopes, want)
	}

	third := createAccount(t, env)
	if err := svc.DeleteUser(context.Background(), &dto.GetDetailRequest{Id: third.Id}); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if len(caches.users) != 2 || !reflect.DeepEqual(caches.users[1], []int64{third.Id}) || len(caches.scopes) != 2 {
		t.Fatalf("single deletion calls = %v, %+v", caches.users, caches.scopes)
	}
}

// Without a node scope, or when the subscriptions cannot be read, there is
// nothing to clear on the network side; the deletion still succeeds.
func TestDeletingAccountsWithoutANodeScopeLeavesTheNodeCaches(t *testing.T) {
	for name, caches := range map[string]*accessCaches{
		"no plan nodes":       {},
		"subscriptions error": {nodeIDs: []int64{5}, subErr: errors.New("database unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			env, svc := newAccessFixture(t, caches)
			account := createAccount(t, env)
			if err := svc.DeleteUser(context.Background(), &dto.GetDetailRequest{Id: account.Id}); err != nil {
				t.Fatalf("DeleteUser() error = %v", err)
			}
			if len(caches.users) != 1 || len(caches.scopes) != 0 {
				t.Fatalf("calls = %v, %+v; want one subscription call and no node call", caches.users, caches.scopes)
			}
		})
	}
}
