package startup

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

type fixture struct {
	*identitytest.Env
	svc *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	return &fixture{Env: env, svc: NewService(Deps{Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Store: env.Store})}
}

// account stores u with one binding of authType, unless identifier is empty.
func (f *fixture) account(t *testing.T, u *user.User, authType, identifier string) *user.User {
	t.Helper()
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if identifier != "" {
		if err := f.DB.Create(&user.AuthMethods{UserId: u.Id, AuthType: authType, AuthIdentifier: identifier, Verified: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return u
}

// A fresh database gets the configured administrator: an administrator
// account signing in with the password, with its refer code and a verified
// email binding.
func TestCreateInitialAdministratorSeedsADatabaseWithoutAccounts(t *testing.T) {
	f := newFixture(t)

	created, err := f.svc.CreateInitialAdministrator(context.Background(), "admin@example.com", "first-secret")

	if err != nil || !created {
		t.Fatalf("CreateInitialAdministrator() = %t, %v, want an administrator created", created, err)
	}
	users := f.Users(t)
	if len(users) != 1 {
		t.Fatalf("accounts = %+v, want the administrator", users)
	}
	admin := users[0]
	if admin.IsAdmin == nil || !*admin.IsAdmin || admin.Algo != password.PasswordAlgoArgon2id ||
		!password.MultiPasswordVerify(admin.Algo, admin.Salt, "first-secret", admin.Password) || !strings.HasPrefix(admin.ReferCode, "u") {
		t.Fatalf("administrator = %+v, want an administrator on the password with a refer code", admin)
	}
	identities := f.Identities(t, admin.Id)
	if len(identities) != 1 || identities[0].AuthType != "email" || identities[0].AuthIdentifier != "admin@example.com" || !identities[0].Verified {
		t.Fatalf("identities = %+v, want the verified email binding", identities)
	}
}

// The configured email is stored in its canonical form, the one sign-in
// looks up, however the operator wrote it; an address that is not one is
// refused before anything is written. Seeding is idempotent: a second run
// against the seeded database creates nothing, so it may run on every start.
func TestCreateInitialAdministratorCanonicalizesTheEmailAndRunsOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if created, err := f.svc.CreateInitialAdministrator(ctx, "not-an-email", "first-secret"); err == nil || created {
		t.Fatalf("CreateInitialAdministrator(invalid email) = %t, %v, want a refusal", created, err)
	}
	if users := f.Users(t); len(users) != 0 {
		t.Fatalf("accounts = %+v, want none after the refusal", users)
	}

	created, err := f.svc.CreateInitialAdministrator(ctx, " Admin@Example.COM ", "first-secret")
	if err != nil || !created {
		t.Fatalf("CreateInitialAdministrator() = %t, %v, want an administrator created", created, err)
	}
	identities := f.Identities(t, f.Users(t)[0].Id)
	if len(identities) != 1 || identities[0].AuthIdentifier != "admin@example.com" {
		t.Fatalf("identities = %+v, want the canonical email", identities)
	}
	if _, err := f.Store.User().FindOneByEmail(ctx, "admin@example.com"); err != nil {
		t.Fatalf("the administrator is not found by its email: %v", err)
	}

	for i := 0; i < 2; i++ {
		if created, err := f.svc.CreateInitialAdministrator(ctx, "admin@example.com", "another-secret"); err != nil || created {
			t.Fatalf("run %d: CreateInitialAdministrator() = %t, %v, want nothing created", i+2, created, err)
		}
	}
	if users := f.Users(t); len(users) != 1 {
		t.Fatalf("accounts = %+v, want the one administrator", users)
	}
}

// Once the database holds an account, no administrator is seeded.
func TestCreateInitialAdministratorLeavesADatabaseWithAccounts(t *testing.T) {
	f := newFixture(t)
	f.account(t, &user.User{}, "email", "someone@example.com")

	created, err := f.svc.CreateInitialAdministrator(context.Background(), "admin@example.com", "first-secret")

	if err != nil || created {
		t.Fatalf("CreateInitialAdministrator() = %t, %v, want nothing created", created, err)
	}
	if users := f.Users(t); len(users) != 1 {
		t.Fatalf("accounts = %+v, want only the existing one", users)
	}
}

// The account and its email binding are created together: when the binding
// cannot be written, the account rolls back and the failure is reported under
// DatabaseInsertError.
func TestCreateInitialAdministratorRollsBackWithoutItsBinding(t *testing.T) {
	f := newFixture(t)
	if err := f.DB.Exec(`CREATE TRIGGER reject_binding BEFORE INSERT ON user_auth_methods BEGIN SELECT RAISE(FAIL, 'test failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	created, err := f.svc.CreateInitialAdministrator(context.Background(), "admin@example.com", "first-secret")

	if created || xerr.CodeOf(err) != xerr.DatabaseInsertError {
		t.Fatalf("CreateInitialAdministrator() = %t, %v (code %d), want DatabaseInsertError", created, err, xerr.CodeOf(err))
	}
	if users := f.Users(t); len(users) != 0 {
		t.Fatalf("accounts = %+v, want the administrator rolled back", users)
	}
}

// Only administrators on the given password are found, whatever hash it is
// stored under, and they come with their auth methods; members and
// administrators on another password are not.
func TestFindAdministratorsWithPasswordMatchesOnlyThatPassword(t *testing.T) {
	f := newFixture(t)
	admin, member := true, false
	onDefault := f.account(t, &user.User{IsAdmin: &admin, Password: password.EncodePassWord("password"), Algo: password.PasswordAlgoArgon2id}, "email", "default@example.com")
	// md5("password"), as a legacy hash.
	legacy := f.account(t, &user.User{IsAdmin: &admin, Password: "5f4dcc3b5aa765d61d8327deb882cf99", Algo: "md5"}, "", "")
	f.account(t, &user.User{IsAdmin: &admin, Password: password.EncodePassWord("a-strong-password"), Algo: password.PasswordAlgoArgon2id}, "email", "strong@example.com")
	f.account(t, &user.User{IsAdmin: &member, Password: password.EncodePassWord("password"), Algo: password.PasswordAlgoArgon2id}, "email", "member@example.com")

	found, err := f.svc.FindAdministratorsWithPassword(context.Background(), "password")

	if err != nil {
		t.Fatalf("FindAdministratorsWithPassword() error = %v", err)
	}
	var ids []int64
	for _, u := range found {
		ids = append(ids, u.Id)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{onDefault.Id, legacy.Id}) {
		t.Fatalf("found %v, want the administrators %d and %d", ids, onDefault.Id, legacy.Id)
	}
	for _, u := range found {
		if u.Id == onDefault.Id && (len(u.AuthMethods) != 1 || u.AuthMethods[0].AuthIdentifier != "default@example.com") {
			t.Fatalf("auth methods = %+v, want the email binding", u.AuthMethods)
		}
	}
}

// Two email bindings that differ only in letter case or surrounding spaces
// make email sign-in ambiguous, and the check refuses them.
func TestValidateEmailIdentitiesRefusesBindingsSignInCannotTellApart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.account(t, &user.User{}, "email", "someone@example.com")
	if err := f.svc.ValidateEmailIdentities(ctx); err != nil {
		t.Fatalf("distinct bindings: ValidateEmailIdentities() = %v", err)
	}

	f.account(t, &user.User{}, "email", " Someone@Example.com")

	if err := f.svc.ValidateEmailIdentities(ctx); xerr.CodeOf(err) != xerr.EmailIdentityAmbiguous {
		t.Fatalf("folded duplicates: ValidateEmailIdentities() = %v, want EmailIdentityAmbiguous", err)
	}
}

// Legacy phone numbers are rewritten to E.164 and the run is summarised; a
// run with nothing left to change logs nothing.
func TestNormalizePhoneNumbersStoresE164AndSummarisesTheRun(t *testing.T) {
	f := newFixture(t)
	logs := logtest.NewCollector(t)
	ctx := context.Background()
	u := f.account(t, &user.User{}, "mobile", "86-13800138000")

	if err := f.svc.NormalizePhoneNumbers(ctx); err != nil {
		t.Fatalf("NormalizePhoneNumbers() = %v", err)
	}

	if identities := f.Identities(t, u.Id); len(identities) != 1 || identities[0].AuthIdentifier != "+8613800138000" {
		t.Fatalf("identities = %+v, want the number in E.164", identities)
	}
	if content := logs.String(); !strings.Contains(content, "normalized stored phone numbers to E.164") || !strings.Contains(content, `"converted":1`) {
		t.Fatalf("log = %s, want the conversion summarised", content)
	}

	logs.Reset()
	if err := f.svc.NormalizePhoneNumbers(ctx); err != nil {
		t.Fatalf("second NormalizePhoneNumbers() = %v", err)
	}
	if content := logs.String(); strings.Contains(content, "normalized stored phone numbers") {
		t.Fatalf("log = %s, want nothing for a run without changes", content)
	}
}
