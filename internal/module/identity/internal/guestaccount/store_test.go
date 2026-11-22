package guestaccount

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// A paid guest order gets an account whose identity nobody verified yet. It
// is not a registration: no registration event (and so no trial) and no
// registration audit.
func TestGuestAccountWritesAnUnverifiedAccountOnly(t *testing.T) {
	env := identitytest.New(t)
	svc := New(env.Store)
	hash := password.EncodePassWord("guest-password")

	id, err := svc.EnsureGuestAccount(context.Background(), Command{
		OrderNo: "order-1", AuthType: "mobile", Identifier: "86-13800138000", PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("EnsureGuestAccount() error = %v", err)
	}
	users := env.Users(t)
	if len(users) != 1 || users[0].Id != id || users[0].Password != hash || users[0].ReferCode == "" {
		t.Fatalf("accounts = %+v", users)
	}
	identities := env.Identities(t, id)
	if len(identities) != 1 || identities[0].AuthIdentifier != "+8613800138000" || identities[0].Verified {
		t.Fatalf("identities = %+v, want the unverified number in E.164", identities)
	}
	if events := env.Events(t, account.RegisteredTopic); len(events) != 0 {
		t.Fatalf("registration events = %d, want none", len(events))
	}
	if rows := env.Logs(t, log.TypeRegister, id); len(rows) != 0 {
		t.Fatalf("registration audits = %d, want none", len(rows))
	}
	again, err := svc.EnsureGuestAccount(context.Background(), Command{OrderNo: "order-1"})
	if err != nil || again != id {
		t.Fatalf("replay = %d, %v; want the same account", again, err)
	}
}
