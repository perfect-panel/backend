package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// An auth-method update writes the configuration and the switch only: a
// snapshot loaded before another change, or one without a switch, cannot
// revert the method name, the creation time or the switch itself.
func TestAuthMethodUpdateWritesOnlyConfigAndSwitch(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	ctx := context.Background()
	enabled := true
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := env.DB.Create(&auth.Auth{Method: "github", Config: `{"client_id":"old"}`, Enabled: &enabled, CreatedAt: created}).Error; err != nil {
		t.Fatal(err)
	}
	stored, err := env.Store.Auth().FindOneByMethod(ctx, "github")
	if err != nil {
		t.Fatal(err)
	}

	// A snapshot with a stale method name and no switch changes the
	// configuration alone.
	snapshot := &auth.Auth{Id: stored.Id, Method: "renamed", Config: `{"client_id":"new"}`}
	if err := env.Store.Auth().Update(ctx, snapshot); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	current, err := env.Store.Auth().FindOne(ctx, stored.Id)
	if err != nil {
		t.Fatal(err)
	}
	if current.Method != "github" || current.Config != `{"client_id":"new"}` || current.Enabled == nil || !*current.Enabled || !current.CreatedAt.Equal(created) {
		t.Fatalf("after the update = %+v, want only the configuration changed", current)
	}

	disabled := false
	if err := env.Store.Auth().Update(ctx, &auth.Auth{Id: stored.Id, Config: current.Config, Enabled: &disabled}); err != nil {
		t.Fatalf("Update() with a switch: %v", err)
	}
	if current, err = env.Store.Auth().FindOne(ctx, stored.Id); err != nil || current.Enabled == nil || *current.Enabled {
		t.Fatalf("after switching off = %+v, %v; want disabled", current, err)
	}
}

// A binding update writes the identifier and the verified flag only, so a
// snapshot cannot move the binding to another account or type.
func TestBindingUpdateWritesOnlyIdentifierAndVerified(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	ctx := context.Background()
	owner := &user.User{}
	if err := env.DB.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := env.DB.Create(&user.AuthMethods{UserId: owner.Id, AuthType: "github", AuthIdentifier: "old-subject", CreatedAt: created}).Error; err != nil {
		t.Fatal(err)
	}
	binding, err := env.Store.UserAuth().FindUserAuthMethodByUserId(ctx, "github", owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	binding.AuthIdentifier, binding.Verified = "new-subject", true
	binding.Id = 0 // a snapshot without its row id must not insert a second row
	if err := env.Store.UserAuth().UpdateUserAuthMethods(ctx, binding); err != nil {
		t.Fatalf("UpdateUserAuthMethods() error = %v", err)
	}
	identities := env.Identities(t, owner.Id)
	if len(identities) != 1 || identities[0].AuthIdentifier != "new-subject" || !identities[0].Verified || identities[0].AuthType != "github" || !identities[0].CreatedAt.Equal(created) {
		t.Fatalf("identities = %+v, want the one binding with its identifier and flag changed", identities)
	}
}
