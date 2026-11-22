package adminuser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// wallets is the billing port of the admin flows: the wallet table the admin
// views read, and a record of the money edits the flows hand to billing,
// which moves the money in its own transaction.
type wallets struct {
	rows     map[int64]*walletEntity.Wallet
	openings []walletEntity.Wallet
	adjusts  []walletEntity.Adjustment
	// failEdits fails the money edits.
	failEdits error
}

// amount is a wallet amount an edit sets.
func amount(v int64) *int64 { return &v }

var _ Wallets = (*wallets)(nil)

func (w *wallets) FindWallet(_ context.Context, userID int64) (*walletEntity.Wallet, error) {
	return w.rows[userID], nil
}

func (w *wallets) FindWallets(_ context.Context, ids []int64) (map[int64]*walletEntity.Wallet, error) {
	found := make(map[int64]*walletEntity.Wallet)
	for _, id := range ids {
		if row, ok := w.rows[id]; ok {
			found[id] = row
		}
	}
	return found, nil
}

func (w *wallets) OpenWallet(_ context.Context, opening walletEntity.Wallet) error {
	w.openings = append(w.openings, opening)
	return w.failEdits
}

func (w *wallets) AdjustWallet(_ context.Context, adjustment walletEntity.Adjustment) error {
	w.adjusts = append(w.adjusts, adjustment)
	return w.failEdits
}

type fixture struct {
	*identitytest.Env
	svc     *Service
	wallets *wallets
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	w := &wallets{rows: map[int64]*walletEntity.Wallet{}}
	svc := NewService(Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Wallet: w,
		Store: env.Store, Redis: env.Redis,
	})
	return &fixture{Env: env, svc: svc, wallets: w}
}

func (f *fixture) account(t *testing.T, authType, identifier string) *user.User {
	t.Helper()
	enabled := true
	u := &user.User{Enable: &enabled, ReferCode: "REF-" + identifier}
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

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// An account the administrator creates keeps its phone number in E.164, the
// form sign-in and password reset look it up in; it used to be stored as
// "<area>-<number>" and never found.
func TestCreateUserStoresThePhoneNumberInE164(t *testing.T) {
	f := newFixture(t)
	err := f.svc.CreateUser(context.Background(), &dto.CreateUserRequest{
		Email: "new@example.com", Password: "password-1", TelephoneAreaCode: "86", Telephone: "13800138000",
		ReferCode: "ADMIN-MADE",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	users := f.Users(t)
	if len(users) != 1 || users[0].ReferCode != "ADMIN-MADE" || !password.MultiPasswordVerify(users[0].Algo, users[0].Salt, "password-1", users[0].Password) {
		t.Fatalf("accounts = %+v", users)
	}
	found, err := f.Store.UserAuth().FindUserAuthMethodByOpenID(context.Background(), "mobile", "+8613800138000")
	if err != nil || found.UserId != users[0].Id {
		t.Fatalf("the E.164 lookup sign-in uses = %+v, %v", found, err)
	}
	if identities := f.Identities(t, users[0].Id); len(identities) != 2 {
		t.Fatalf("identities = %+v, want the phone number and the email", identities)
	}
	if len(f.wallets.openings) != 0 {
		t.Fatalf("wallet openings = %+v, want none for an account without money", f.wallets.openings)
	}

	list, err := f.svc.GetUserList(context.Background(), &dto.GetUserListRequest{Page: 1, Size: 10})
	if err != nil || len(list.List) != 1 {
		t.Fatalf("GetUserList = %+v, %v", list, err)
	}
	for _, method := range list.List[0].AuthMethods {
		if method.AuthType == "mobile" && method.AuthIdentifier != "+86 138 0013 8000" {
			t.Fatalf("listed number = %q, want the international form", method.AuthIdentifier)
		}
	}
}

// An account created without a password gets one nobody knows, so it signs
// in only after a password reset or through another method. The address used
// to be the password, and so, since phone numbers are stored in E.164, was
// the number of an account with one: anyone who knew the identifier could
// sign in.
func TestCreateUserWithoutAPasswordGetsOneNobodyKnows(t *testing.T) {
	f := newFixture(t)
	err := f.svc.CreateUser(context.Background(), &dto.CreateUserRequest{
		Email: "new@example.com", TelephoneAreaCode: "86", Telephone: "13800138000",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	users := f.Users(t)
	if len(users) != 1 || !strings.HasPrefix(users[0].Password, "$argon2id$") || users[0].Algo != password.PasswordAlgoArgon2id {
		t.Fatalf("accounts = %+v, want one with an argon2id password", users)
	}
	for _, guess := range []string{"new@example.com", "New@Example.com", "+8613800138000", "8613800138000", "13800138000", "86-13800138000", ""} {
		if password.MultiPasswordVerify(users[0].Algo, users[0].Salt, guess, users[0].Password) {
			t.Fatalf("%q signs in to the account created without a password", guess)
		}
	}
}

// A number or address another account holds is refused, whatever form the
// administrator types it in.
func TestCreateUserRefusesTakenIdentifiers(t *testing.T) {
	f := newFixture(t)
	f.account(t, "mobile", "+8613800138000")
	f.account(t, "email", "taken@example.com")

	err := f.svc.CreateUser(context.Background(), &dto.CreateUserRequest{TelephoneAreaCode: "86", Telephone: "138-0013-8000"})
	assertCode(t, err, xerr.TelephoneExist)
	err = f.svc.CreateUser(context.Background(), &dto.CreateUserRequest{Email: "TAKEN@example.com"})
	assertCode(t, err, xerr.EmailExist)
	err = f.svc.CreateUser(context.Background(), &dto.CreateUserRequest{TelephoneAreaCode: "86", Telephone: "not a number"})
	assertCode(t, err, xerr.TelephoneError)
	if n := len(f.Users(t)); n != 2 {
		t.Fatalf("accounts = %d, want only the existing two", n)
	}
}

// The opening money is billing's: once the identity transaction created the
// account, billing credits the new account's wallet in its own transaction.
// A failure there leaves the uncredited account for the administrator to
// adjust.
func TestCreateUserCreditsTheOpeningWalletAfterTheAccount(t *testing.T) {
	f := newFixture(t)
	req := &dto.CreateUserRequest{Email: "rich@example.com", Password: "password-1", Balance: 500, GiftAmount: 200, Commission: 50}
	if err := f.svc.CreateUser(context.Background(), req); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	users := f.Users(t)
	if len(users) != 1 {
		t.Fatalf("accounts = %+v", users)
	}
	want := walletEntity.Wallet{UserId: users[0].Id, Balance: 500, GiftAmount: 200, Commission: 50}
	if len(f.wallets.openings) != 1 || f.wallets.openings[0] != want {
		t.Fatalf("wallet openings = %+v, want %+v", f.wallets.openings, want)
	}

	f.wallets.failEdits = errors.New("billing unavailable")
	req.Email = "unlucky@example.com"
	if err := f.svc.CreateUser(context.Background(), req); !errors.Is(err, f.wallets.failEdits) {
		t.Fatalf("CreateUser() error = %v, want the billing failure", err)
	}
	if n := len(f.Users(t)); n != 2 {
		t.Fatalf("accounts = %d, want the uncredited account kept", n)
	}
}

// errUnavailable is the failure of an identity table that cannot be read.
var errUnavailable = errors.New("database unavailable")

// failingAuths is an identity table that cannot be read.
type failingAuths struct{}

var _ UserAuths = failingAuths{}

func (failingAuths) FindUserAuthMethods(context.Context, int64) ([]*user.AuthMethods, error) {
	return nil, errUnavailable
}

func (failingAuths) FindUserAuthMethodByOpenID(context.Context, string, string) (*user.AuthMethods, error) {
	return &user.AuthMethods{}, errUnavailable
}

func (failingAuths) FindUserAuthMethodByPlatform(context.Context, int64, string) (*user.AuthMethods, error) {
	return nil, errUnavailable
}

func (failingAuths) UpdateUserAuthMethods(context.Context, *user.AuthMethods) error {
	return errUnavailable
}

func (failingAuths) DeleteUserAuthMethods(context.Context, int64, string) error {
	return errUnavailable
}

// A failed duplicate check is a database error, not a free identifier.
func TestCreateUserReportsAFailedDuplicateCheck(t *testing.T) {
	f := newFixture(t)
	f.svc.deps.UserAuths = failingAuths{}
	err := f.svc.CreateUser(context.Background(), &dto.CreateUserRequest{TelephoneAreaCode: "86", Telephone: "13800138000"})
	assertCode(t, err, xerr.DatabaseQueryError)
	if n := len(f.Users(t)); n != 0 {
		t.Fatalf("accounts = %d, want none", n)
	}
}

// A rejected edit reports why: the transaction error used to be replaced by
// a database error.
func TestUpdateUserBasicInfoReportsValidationCodes(t *testing.T) {
	f := newFixture(t)
	target := f.account(t, "email", "owner@example.com")

	err := f.svc.UpdateUserBasicInfo(context.Background(), &dto.UpdateUserBasicInfoRequest{UserId: target.Id, Avatar: "not-an-image", Enable: true, Balance: amount(100)})
	assertCode(t, err, xerr.InvalidParams)
	if len(f.wallets.adjusts) != 0 {
		t.Fatalf("wallet adjustments = %+v, want none after a rejected edit", f.wallets.adjusts)
	}

	t.Setenv("PPANEL_MODE", "demo")
	demoAdmin := f.account(t, "email", "admin@example.com")
	if demoAdmin.Id != demoAdminID {
		t.Fatalf("demo admin id = %d, want %d", demoAdmin.Id, demoAdminID)
	}
	err = f.svc.UpdateUserBasicInfo(context.Background(), &dto.UpdateUserBasicInfoRequest{UserId: demoAdmin.Id, Password: "new-password", Enable: true})
	assertCode(t, err, xerr.DemoModeRestricted)
}

func TestUpdateUserBasicInfoWritesTheProfile(t *testing.T) {
	f := newFixture(t)
	target := f.account(t, "email", "owner@example.com")

	err := f.svc.UpdateUserBasicInfo(context.Background(), &dto.UpdateUserBasicInfoRequest{
		UserId: target.Id, ReferCode: "RENAMED", Enable: true, Password: "new-password",
	})
	if err != nil {
		t.Fatalf("UpdateUserBasicInfo() error = %v", err)
	}
	var stored user.User
	if err := f.DB.First(&stored, target.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReferCode != "RENAMED" || !password.MultiPasswordVerify(stored.Algo, stored.Salt, "new-password", stored.Password) {
		t.Fatalf("stored = %+v", stored)
	}
	// The wallet amounts of the edit go to billing; an edit that carries
	// none sets none, so the wallet is left alone.
	if want := (walletEntity.Adjustment{UserId: target.Id}); len(f.wallets.adjusts) != 1 || !reflect.DeepEqual(f.wallets.adjusts[0], want) {
		t.Fatalf("wallet adjustments = %+v, want %+v", f.wallets.adjusts, want)
	}
	if rows := f.Logs(t, log.TypeBalance, target.Id); len(rows) != 0 {
		t.Fatalf("balance audits = %d, want none in identity", len(rows))
	}
}

// The money adjustment runs after the profile committed: a failed one keeps
// the profile edit and reports a database update failure for the
// administrator to retry.
func TestUpdateUserBasicInfoAdjustsTheWalletAfterTheProfile(t *testing.T) {
	f := newFixture(t)
	target := f.account(t, "email", "owner@example.com")
	f.wallets.failEdits = errors.New("billing unavailable")

	err := f.svc.UpdateUserBasicInfo(context.Background(), &dto.UpdateUserBasicInfoRequest{
		UserId: target.Id, ReferCode: "RENAMED", Enable: true, Balance: amount(900), GiftAmount: amount(30), Commission: amount(7),
	})
	assertCode(t, err, xerr.DatabaseUpdateError)
	if !errors.Is(err, f.wallets.failEdits) {
		t.Fatalf("error = %v, want the billing failure", err)
	}
	want := walletEntity.Adjustment{UserId: target.Id, Balance: amount(900), GiftAmount: amount(30), Commission: amount(7)}
	if len(f.wallets.adjusts) != 1 || !reflect.DeepEqual(f.wallets.adjusts[0], want) {
		t.Fatalf("wallet adjustments = %+v, want %+v", f.wallets.adjusts, want)
	}
	var stored user.User
	if err := f.DB.First(&stored, target.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReferCode != "RENAMED" {
		t.Fatalf("stored = %+v, want the committed profile edit", stored)
	}
}

// An edit that carries only some of the wallet amounts hands billing only
// those: the amounts left out of the request are not set to zero, so a form
// that omits them cannot revert the money movements made since it was loaded.
func TestUpdateUserBasicInfoForwardsOnlyTheAmountsSent(t *testing.T) {
	f := newFixture(t)
	target := f.account(t, "email", "owner@example.com")

	var req dto.UpdateUserBasicInfoRequest
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"user_id":%d,"enable":true,"gift_amount":30}`, target.Id)), &req); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateUserBasicInfo(context.Background(), &req); err != nil {
		t.Fatalf("UpdateUserBasicInfo() error = %v", err)
	}
	want := walletEntity.Adjustment{UserId: target.Id, GiftAmount: amount(30)}
	if len(f.wallets.adjusts) != 1 || !reflect.DeepEqual(f.wallets.adjusts[0], want) {
		t.Fatalf("wallet adjustments = %+v, want only the gift amount set", f.wallets.adjusts)
	}
}

// The demo instance's administrator cannot be deleted.
func TestDeletingTheDemoAdministratorIsRefused(t *testing.T) {
	f := newFixture(t)
	f.account(t, "email", "first@example.com")
	admin := f.account(t, "email", "admin@example.com")
	t.Setenv("PPANEL_MODE", "demo")

	assertCode(t, f.svc.DeleteUser(context.Background(), &dto.GetDetailRequest{Id: admin.Id}), xerr.DemoModeRestricted)
	assertCode(t, f.svc.BatchDeleteUser(context.Background(), &dto.BatchDeleteUserRequest{Ids: []int64{1, admin.Id}}), xerr.DemoModeRestricted)
	for _, u := range f.Users(t) {
		if u.DeletedAt.Valid {
			t.Fatalf("user %d was deleted", u.Id)
		}
	}
	t.Setenv("PPANEL_MODE", "")
	if err := f.svc.DeleteUser(context.Background(), &dto.GetDetailRequest{Id: admin.Id}); err != nil {
		t.Fatalf("DeleteUser() outside demo mode: %v", err)
	}
}

// An administrator's binding is normalized like a self-service one, and a
// number that cannot be is refused.
func TestCreateUserAuthMethodNormalizesTheIdentifier(t *testing.T) {
	f := newFixture(t)
	target := f.account(t, "email", "")

	if err := f.svc.CreateUserAuthMethod(context.Background(), &dto.CreateUserAuthMethodRequest{UserId: target.Id, AuthType: "mobile", AuthIdentifier: "86-13800138000"}); err != nil {
		t.Fatalf("CreateUserAuthMethod() error = %v", err)
	}
	identities := f.Identities(t, target.Id)
	if len(identities) != 1 || identities[0].AuthIdentifier != "+8613800138000" || !identities[0].Verified {
		t.Fatalf("identities = %+v", identities)
	}
	err := f.svc.UpdateUserAuthMethod(context.Background(), &dto.UpdateUserAuthMethodRequest{UserId: target.Id, AuthType: "mobile", AuthIdentifier: "not a number"})
	assertCode(t, err, xerr.TelephoneError)
	if err := f.svc.UpdateUserAuthMethod(context.Background(), &dto.UpdateUserAuthMethodRequest{UserId: target.Id, AuthType: "mobile", AuthIdentifier: "+86 139 0013 9000"}); err != nil {
		t.Fatalf("UpdateUserAuthMethod() error = %v", err)
	}
	if identities := f.Identities(t, target.Id); identities[0].AuthIdentifier != "+8613900139000" {
		t.Fatalf("identities = %+v", identities)
	}
	err = f.svc.CreateUserAuthMethod(context.Background(), &dto.CreateUserAuthMethodRequest{UserId: target.Id, AuthType: "mobile", AuthIdentifier: "call-me"})
	assertCode(t, err, xerr.TelephoneError)
	err = f.svc.CreateUserAuthMethod(context.Background(), &dto.CreateUserAuthMethodRequest{UserId: target.Id, AuthType: "Device", AuthIdentifier: "device-1"})
	assertCode(t, err, xerr.InvalidParams)
}
