package repo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/repo"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// authGate is the uncached gate read the module's repository adds to the
// shared contract.
type authGate interface {
	FindAccountStateForAuth(ctx context.Context, id int64) (*user.AccountState, error)
}

var _ authGate = (*repo.UserRepo)(nil)

// The account gate request authentication reads comes from the database,
// not the cache: a ban, deletion or demotion written behind the cache's back
// is seen at once, while the cached row still holds the old account.
func TestFindAccountStateForAuthBypassesTheCache(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	ctx := context.Background()
	enabled, admin := true, true
	u := &user.User{Enable: &enabled, IsAdmin: &admin}
	if err := env.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	users := env.Store.User()
	if _, err := users.FindOne(ctx, u.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := users.FindAccountState(ctx, u.Id); err != nil {
		t.Fatal(err)
	}
	state, err := users.(authGate).FindAccountStateForAuth(ctx, u.Id)
	if err != nil || state.Enable == nil || !*state.Enable || state.IsAdmin == nil || !*state.IsAdmin || state.DeletedAt.Valid {
		t.Fatalf("state = %+v, %v; want an enabled administrator", state, err)
	}

	if err := env.DB.Exec("UPDATE user SET enable = false, is_admin = false, deleted_at = CURRENT_TIMESTAMP WHERE id = ?", u.Id).Error; err != nil {
		t.Fatal(err)
	}
	state, err = users.(authGate).FindAccountStateForAuth(ctx, u.Id)
	if err != nil || state.Enable == nil || *state.Enable || state.IsAdmin == nil || *state.IsAdmin || !state.DeletedAt.Valid {
		t.Fatalf("state after the change = %+v, %v; want disabled, demoted and deleted", state, err)
	}
	// The cached reads still serve the old row: that is why the gate does
	// not use them.
	if cached, err := users.FindOne(ctx, u.Id); err != nil || cached.Enable == nil || !*cached.Enable {
		t.Fatalf("cached account = %+v, %v; want the stale enabled row", cached, err)
	}
	if cached, err := users.FindAccountState(ctx, u.Id); err != nil || cached.Enable == nil || !*cached.Enable {
		t.Fatalf("cached state = %+v, %v; want the stale enabled row", cached, err)
	}
}

// A write that changes the account gate whose cache invalidation fails is
// retried: once Redis is back the cached rows go, so a ban is not served
// from the cache for its lifetime.
func TestFailedInvalidationsOfAccountWritesAreRetried(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	ctx := context.Background()
	enabled := true
	u := &user.User{Enable: &enabled}
	if err := env.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	users := env.Store.User()
	if _, err := users.FindOne(ctx, u.Id); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("cache:user:id:%d", u.Id)
	if !env.Mini.Exists(key) {
		t.Fatal("the account row was not cached")
	}

	env.Mini.SetError("redis unavailable")
	if err := users.UpdateColumns(ctx, u.Id, map[string]any{"enable": false}); err != nil {
		t.Fatalf("UpdateColumns() with Redis down: %v; the database write must stand", err)
	}
	env.Mini.SetError("")
	if !env.Mini.Exists(key) {
		t.Fatal("the cached row went although the invalidation failed; the test needs the failure")
	}

	deadline := time.Now().Add(5 * time.Second)
	for env.Mini.Exists(key) {
		if time.Now().After(deadline) {
			t.Fatal("the failed invalidation was never retried")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if fresh, err := users.FindOne(ctx, u.Id); err != nil || fresh.Enable == nil || *fresh.Enable {
		t.Fatalf("account after the retry = %+v, %v; want the ban", fresh, err)
	}
}

// Deleting accounts invalidates their cached rows too, with the same retry.
func TestDeletionsInvalidateTheCachedRows(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	ctx := context.Background()
	enabled := true
	first, second := &user.User{Enable: &enabled}, &user.User{Enable: &enabled}
	for _, u := range []*user.User{first, second} {
		if err := env.DB.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	users := env.Store.User()
	for _, u := range []*user.User{first, second} {
		if _, err := users.FindOne(ctx, u.Id); err != nil {
			t.Fatal(err)
		}
	}
	if err := users.Delete(ctx, first.Id); err != nil {
		t.Fatal(err)
	}
	if err := users.BatchDeleteUser(ctx, []int64{second.Id}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*user.User{first, second} {
		if env.Mini.Exists(fmt.Sprintf("cache:user:id:%d", u.Id)) {
			t.Fatalf("the cached row of deleted account %d survived", u.Id)
		}
		if deleted, err := users.FindOne(ctx, u.Id); err != nil || !deleted.DeletedAt.Valid {
			t.Fatalf("account %d = %+v, %v; want it deleted", u.Id, deleted, err)
		}
	}
}
