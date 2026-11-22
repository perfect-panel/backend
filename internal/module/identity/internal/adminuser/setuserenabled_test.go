package adminuser

import (
	"context"
	"fmt"
	"testing"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
)

// The bot's ban and unban write only the enable flag and drop the account's
// cached rows, so the next account read already sees the new state.
func TestSetUserEnabledWritesOnlyTheFlag(t *testing.T) {
	f := newFixture(t)
	u := f.account(t, "email", "ban@example.com")
	ctx := context.Background()
	if _, err := f.Store.User().FindAccountState(ctx, u.Id); err != nil {
		t.Fatal(err)
	}
	cached := fmt.Sprintf("cache:user:state:%d", u.Id)
	if !f.Mini.Exists(cached) {
		t.Fatalf("the account state was not cached under %s", cached)
	}

	if err := f.svc.SetUserEnabled(ctx, u.Id, false); err != nil {
		t.Fatalf("ban: %v", err)
	}
	if f.Mini.Exists(cached) {
		t.Fatal("the cached account state survived the ban")
	}
	state, err := f.Store.User().FindAccountState(ctx, u.Id)
	if err != nil || state.Enable == nil || *state.Enable {
		t.Fatalf("state after the ban = %+v, %v; want disabled", state, err)
	}
	var stored user.User
	if err := f.DB.First(&stored, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReferCode != u.ReferCode || stored.IsAdmin == nil || *stored.IsAdmin {
		t.Fatalf("the ban changed other columns: %+v", stored)
	}

	if err := f.svc.SetUserEnabled(ctx, u.Id, true); err != nil {
		t.Fatalf("unban: %v", err)
	}
	state, err = f.Store.User().FindAccountState(ctx, u.Id)
	if err != nil || state.Enable == nil || !*state.Enable {
		t.Fatalf("state after the unban = %+v, %v; want enabled", state, err)
	}
}
