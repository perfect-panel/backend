package guestaccount

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// countingStore is the real identity store, counting the identity
// transactions the flow opens.
type countingStore struct {
	store        *repository.GormStore
	transactions int
}

var _ Store = (*countingStore)(nil)

func (s *countingStore) Inbox() repository.InboxRepo { return s.store.Inbox() }

func (s *countingStore) InIdentityTx(ctx context.Context, fn func(repository.IdentityStore) error) error {
	s.transactions++
	return s.store.InIdentityTx(ctx, fn)
}

type guestFixture struct {
	*identitytest.Env
	store *countingStore
	svc   *Service
}

func newGuestFixture(t *testing.T) *guestFixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	store := &countingStore{store: env.Store}
	return &guestFixture{Env: env, store: store, svc: New(store)}
}

// markers returns the guest account markers, the account id by order number.
func (f *guestFixture) markers(t *testing.T) map[string]string {
	t.Helper()
	var rows []inbox.Record
	if err := f.DB.Where("consumer = ?", Consumer).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	markers := make(map[string]string, len(rows))
	for _, row := range rows {
		markers[row.EventKey] = row.Result
	}
	return markers
}

// assertNothingWritten fails when an account, an identity or a marker exists.
func (f *guestFixture) assertNothingWritten(t *testing.T) {
	t.Helper()
	var identities int64
	if err := f.DB.Model(&user.AuthMethods{}).Count(&identities).Error; err != nil {
		t.Fatal(err)
	}
	if users, markers := f.Users(t), f.markers(t); len(users) != 0 || identities != 0 || len(markers) != 0 {
		t.Fatalf("accounts = %d, identities = %d, markers = %v; want nothing written", len(users), identities, markers)
	}
}

// failWrites makes the table refuse the statement (INSERT or UPDATE) until
// the returned function runs.
func (f *guestFixture) failWrites(t *testing.T, table, statement string) (restore func()) {
	t.Helper()
	trigger := "fail_" + table
	create := fmt.Sprintf(`CREATE TRIGGER %s BEFORE %s ON %q BEGIN SELECT RAISE(FAIL, 'guest write failed'); END`, trigger, statement, table)
	if err := f.DB.Exec(create).Error; err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := f.DB.Exec("DROP TRIGGER " + trigger).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuestAccountReplayKeepsIdentityAndPasswordHash(t *testing.T) {
	f := newGuestFixture(t)
	referer := &user.User{ReferCode: "referral"}
	if err := f.DB.Create(referer).Error; err != nil {
		t.Fatal(err)
	}
	hash := password.EncodePassWord("guest-password")
	command := Command{OrderNo: "order-1", AuthType: "email", Identifier: "guest@example.test", PasswordHash: hash, InviteCode: "referral"}
	id, err := f.svc.EnsureGuestAccount(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	var account user.User
	if err := f.DB.First(&account, id).Error; err != nil {
		t.Fatal(err)
	}
	if account.Password != hash || account.Algo != password.PasswordAlgoForHash(hash) || account.RefererId != referer.Id || account.ReferCode == "" {
		t.Fatalf("account = %+v, want its password hash, the referer and a refer code", account)
	}
	identities := f.Identities(t, id)
	if len(identities) != 1 || identities[0].AuthIdentifier != command.Identifier || identities[0].AuthType != command.AuthType {
		t.Fatalf("identities = %+v, want the guest's email", identities)
	}
	if markers := f.markers(t); markers["order-1"] != strconv.FormatInt(id, 10) {
		t.Fatalf("markers = %v, want order-1 marked with account %d", markers, id)
	}
	replayed, err := f.svc.EnsureGuestAccount(context.Background(), Command{OrderNo: command.OrderNo})
	if err != nil || replayed != id || f.store.transactions != 1 {
		t.Fatalf("replay re-created account: id=%d tx=%d err=%v", replayed, f.store.transactions, err)
	}
}

func TestGuestAccountLegacyPasswordIsHashedByIdentity(t *testing.T) {
	f := newGuestFixture(t)
	id, err := f.svc.EnsureGuestAccount(context.Background(), Command{OrderNo: "legacy", AuthType: "email", Identifier: "legacy@example.test", LegacyPassword: "old-password"})
	if err != nil {
		t.Fatal(err)
	}
	var account user.User
	if err := f.DB.First(&account, id).Error; err != nil {
		t.Fatal(err)
	}
	if account.Password == "old-password" || !password.VerifyPassWord("old-password", account.Password) {
		t.Fatal("legacy plaintext was not converted to a valid password hash")
	}
}

func TestGuestAccountMissingPasswordDoesNotCreateAccount(t *testing.T) {
	f := newGuestFixture(t)
	_, err := f.svc.EnsureGuestAccount(context.Background(), Command{
		OrderNo: "missing-password", AuthType: "email", Identifier: "guest@example.test",
	})
	if err == nil {
		t.Fatal("missing credentials must not be converted into an empty-password account")
	}
	if f.store.transactions != 0 {
		t.Fatalf("transactions = %d, want the flow to fail before any identity write", f.store.transactions)
	}
	f.assertNothingWritten(t)
}

func TestGuestAccountFailureRollsBackAccountAuthAndMarker(t *testing.T) {
	for failure, write := range map[string]struct{ table, statement string }{
		// The refer code is set on the new account row.
		"user":  {"user", "UPDATE"},
		"auth":  {"user_auth_methods", "INSERT"},
		"inbox": {"domain_event_inbox", "INSERT"},
	} {
		t.Run(failure, func(t *testing.T) {
			f := newGuestFixture(t)
			restore := f.failWrites(t, write.table, write.statement)
			command := Command{OrderNo: "retry", AuthType: "email", Identifier: "retry@example.test", PasswordHash: "stored-hash"}
			if _, err := f.svc.EnsureGuestAccount(context.Background(), command); err == nil || !strings.Contains(xerr.Detail(err), "guest write failed") {
				t.Fatalf("expected write failure, got %v", err)
			}
			f.assertNothingWritten(t)
			restore()
			id, err := f.svc.EnsureGuestAccount(context.Background(), command)
			if err != nil {
				t.Fatal(err)
			}
			if users, identities, markers := f.Users(t), f.Identities(t, id), f.markers(t); len(users) != 1 || len(identities) != 1 || len(markers) != 1 {
				t.Fatalf("accounts = %d, identities = %d, markers = %v; retry did not commit the complete account", len(users), len(identities), markers)
			}
		})
	}
}

func TestGuestAccountCorruptMarkerDoesNotCreateAccount(t *testing.T) {
	f := newGuestFixture(t)
	if err := f.DB.Create(&inbox.Record{Consumer: Consumer, EventKey: "corrupt", Result: "not-an-id"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EnsureGuestAccount(context.Background(), Command{OrderNo: "corrupt"}); err == nil {
		t.Fatal("corrupt durable marker was accepted")
	}
	if users := f.Users(t); f.store.transactions != 0 || len(users) != 0 {
		t.Fatalf("transactions = %d, accounts = %d; corrupt marker caused account creation", f.store.transactions, len(users))
	}
}

// A guest names an identifier nobody verified; a provider id there would
// pre-claim someone else's OAuth sign-in, so only email and mobile pass.
func TestGuestAccountRejectsProviderIdentities(t *testing.T) {
	f := newGuestFixture(t)
	for _, authType := range []string{"github", "telegram", "google", "apple", "device"} {
		_, err := f.svc.EnsureGuestAccount(context.Background(), Command{
			OrderNo: "order-" + authType, AuthType: authType, Identifier: "583231", PasswordHash: password.EncodePassWord("guest-password"),
		})
		if err == nil {
			t.Fatalf("guest account created for auth type %q", authType)
		}
	}
	if f.store.transactions != 0 {
		t.Fatalf("transactions = %d, want none for provider identities", f.store.transactions)
	}
	f.assertNothingWritten(t)
}
