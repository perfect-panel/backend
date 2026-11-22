package identity

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// The facade's account gate for request authentication reads the row as
// stored: a ban, deletion or demotion written behind the cache's back shows
// at once, while the cached account still holds the old row.
func TestFindAccountStateForAuthReadsTheStoredRow(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	ctx := context.Background()
	enabled, admin := true, true
	u := &user.User{Enable: &enabled, IsAdmin: &admin}
	if err := env.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
	})
	if _, err := svc.FindUser(ctx, u.Id); err != nil {
		t.Fatal(err)
	}
	state, err := svc.FindAccountStateForAuth(ctx, u.Id)
	if err != nil || state.Enable == nil || !*state.Enable || state.IsAdmin == nil || !*state.IsAdmin {
		t.Fatalf("state = %+v, %v; want an enabled administrator", state, err)
	}

	if err := env.DB.Exec("UPDATE user SET enable = false, is_admin = false WHERE id = ?", u.Id).Error; err != nil {
		t.Fatal(err)
	}
	state, err = svc.FindAccountStateForAuth(ctx, u.Id)
	if err != nil || *state.Enable || *state.IsAdmin {
		t.Fatalf("state after the change = %+v, %v; want disabled and demoted", state, err)
	}
	if cached, err := svc.FindUser(ctx, u.Id); err != nil || !*cached.Enable {
		t.Fatalf("cached account = %+v, %v; want the stale row the gate must not trust", cached, err)
	}
}
