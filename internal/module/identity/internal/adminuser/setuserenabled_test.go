package adminuser

import (
	"context"
	"fmt"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
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

func TestAccountReactivationRequiresFreshSessions(t *testing.T) {
	for _, adminEdit := range []bool{false, true} {
		testName := fmt.Sprintf("admin-edit=%t", adminEdit)
		t.Run(testName, func(t *testing.T) {
			f := newFixture(t)
			ctx := t.Context()
			u := f.account(t, "email", "revocation@example.com")
			toggle := func(enabled bool) error {
				if adminEdit {
					return f.svc.UpdateUserBasicInfo(ctx, &dto.UpdateUserBasicInfoRequest{UserId: u.Id, Enable: enabled, ReferCode: u.ReferCode})
				}
				return f.svc.SetUserEnabled(ctx, u.Id, enabled)
			}
			const secret = "account-regression-test-only"
			issue := func() string {
				t.Helper()
				token, err := usersession.Issue(ctx, f.Redis, secret, 3600, usersession.Grant{UserID: u.Id, LoginType: "email"})
				if err != nil {
					t.Fatal(err)
				}
				return token
			}
			old := issue()
			if err := toggle(false); err != nil {
				t.Fatal(err)
			}
			if _, err := usersession.Validate(ctx, f.Redis, secret, old); err == nil {
				t.Fatal("disabled account retained old session")
			}
			// Simulate a session left by a pre-upgrade disable path. No epoch
			// store must mean no successful unban.
			stale := issue()
			f.svc.deps.Redis = nil
			if err := toggle(true); err == nil {
				t.Fatal("unban succeeded without revocation store")
			}
			var state user.User
			err := f.DB.First(&state, u.Id).Error
			if err != nil || state.Enable == nil || *state.Enable {
				t.Fatalf("failed unban changed database state: %+v, %v", state, err)
			}
			f.svc.deps.Redis = f.Redis
			if err := toggle(true); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{old, stale} {
				if _, err := usersession.Validate(ctx, f.Redis, secret, token); err == nil {
					t.Fatal("stale session survived unban")
				}
			}
			fresh := issue()
			if err := toggle(true); err != nil {
				t.Fatal(err)
			}
			if _, err := usersession.Validate(ctx, f.Redis, secret, fresh); err != nil {
				t.Fatalf("new sign-in after unban: %v", err)
			}
		})
	}
}

func TestDisableDuringSessionStoreOutage(t *testing.T) {
	f := newFixture(t)
	u := f.account(t, "email", "offline-revoke@example.com")
	ctx := t.Context()
	const secret = "offline-session-test"
	old, err := usersession.Issue(ctx, f.Redis, secret, 3600, usersession.Grant{UserID: u.Id, LoginType: "email"})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.deps.Redis = nil
	if err := f.svc.SetUserEnabled(ctx, u.Id, false); err != nil {
		t.Fatal(err)
	}
	var banned user.User
	if err := f.DB.First(&banned, u.Id).Error; err != nil || banned.Enable == nil || *banned.Enable {
		t.Fatalf("ban requires a committed disabled flag: %v", err)
	}
	if err := f.svc.SetUserEnabled(ctx, u.Id, true); err == nil {
		t.Fatal("outage must not reinstate pre-ban sessions")
	}
	f.svc.deps.Redis = f.Redis
	if err := f.svc.SetUserEnabled(ctx, u.Id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := usersession.Validate(ctx, f.Redis, secret, old); err == nil {
		t.Fatal("deferred revocation was lost")
	}
}
