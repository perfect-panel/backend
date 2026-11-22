package profile

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// emptyWallets is a wallet table without rows.
type emptyWallets struct{}

func (emptyWallets) FindWallet(context.Context, int64) (*wallet.Wallet, error) { return nil, nil }

// An account's own views mask the identifiers that sign it in elsewhere: a
// device identifier signs the device in, so a web session reading it back
// could sign in as the device; a provider subject is a lookup key at the
// provider. The email address stays readable.
func TestOwnViewsMaskDeviceAndProviderIdentifiers(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	svc := NewService(Deps{
		Users: env.Store.User(), UserAuth: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		UserCache: env.Store.UserCache(), Redis: env.Redis, Store: env.Store, Wallet: emptyWallets{},
	})
	enabled := true
	owner := &user.User{Enable: &enabled}
	if err := env.DB.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	const deviceID = "3f2a9c1d-7b4e-4d2a-9f0e-secret-device"
	for _, binding := range []user.AuthMethods{
		{UserId: owner.Id, AuthType: "email", AuthIdentifier: "owner@example.com", Verified: true},
		{UserId: owner.Id, AuthType: "mobile", AuthIdentifier: "+8613800138000", Verified: true},
		{UserId: owner.Id, AuthType: "github", AuthIdentifier: "583231987", Verified: true},
		{UserId: owner.Id, AuthType: "device", AuthIdentifier: deviceID, Verified: true},
	} {
		if err := env.DB.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := env.DB.Create(&user.Device{UserId: owner.Id, Identifier: deviceID, Enabled: true, Ip: "192.0.2.1"}).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := env.Store.User().FindOne(context.Background(), owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	ctx := user.NewContext(identitytest.Context(), loaded)

	methods, err := svc.GetOAuthMethods(ctx)
	if err != nil {
		t.Fatalf("GetOAuthMethods() error = %v", err)
	}
	devices, err := svc.GetDeviceList(ctx)
	if err != nil {
		t.Fatalf("GetDeviceList() error = %v", err)
	}
	info, err := svc.QueryUserInfo(ctx)
	if err != nil {
		t.Fatalf("QueryUserInfo() error = %v", err)
	}
	for name, view := range map[string]any{"oauth methods": methods, "devices": devices, "account": info} {
		encoded, _ := json.Marshal(view)
		if s := string(encoded); containsAny(s, deviceID, "583231987", "13800138000") {
			t.Fatalf("%s view = %s, want the device, provider and phone identifiers masked", name, s)
		}
	}
	byType := map[string]string{}
	for _, method := range methods.Methods {
		byType[method.AuthType] = method.AuthIdentifier
	}
	if byType["email"] != "owner@example.com" || byType["device"] != "3f2a********" || byType["github"] != "583***987" {
		t.Fatalf("masked methods = %v", byType)
	}
	if len(devices.List) != 1 || devices.List[0].Identifier != "3f2a********" {
		t.Fatalf("masked devices = %+v", devices.List)
	}
	if len(info.UserDevices) != 1 || info.UserDevices[0].Identifier != "3f2a********" {
		t.Fatalf("masked account devices = %+v", info.UserDevices)
	}
}

func containsAny(s string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// A short identifier is hidden whole; a long one keeps a prefix to tell
// devices apart.
func TestMaskDeviceIdentifier(t *testing.T) {
	for id, want := range map[string]string{"": "***", "short-id": "***", "twelve-chars": "***", "thirteen-chars": "thir********", "设备标识符很长很长很长很长的一个": "设备标识********"} {
		if got := maskDeviceIdentifier(id); got != want {
			t.Errorf("maskDeviceIdentifier(%q) = %q, want %q", id, got, want)
		}
	}
}

// A password change is audited in the login history, reports the
// third-party bindings it leaves in place and tells the account about it.
func TestUpdateUserPasswordReportsBindingsAndAudits(t *testing.T) {
	f := newRebindFixture(t)
	owner := f.account(t, "old-password")
	for _, binding := range []user.AuthMethods{
		{UserId: owner.Id, AuthType: "telegram", AuthIdentifier: "10001", Verified: true},
		{UserId: owner.Id, AuthType: "email", AuthIdentifier: "owner@example.com", Verified: true},
	} {
		if err := f.DB.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	f.svc.deps.Logs = f.Store.Log()
	var notified []string
	f.svc.deps.NotifyPasswordChanged = func(_ context.Context, userID int64, bindings []string) error {
		notified = bindings
		return errors.New("no telegram binding")
	}

	resp, err := f.svc.UpdateUserPassword(user.NewContext(identitytest.Context(), owner), &dto.UpdateUserPasswordRequest{OldPassword: "old-password", Password: "new-password-1"})
	if err != nil {
		t.Fatalf("UpdateUserPassword() error = %v", err)
	}
	if !slices.Equal(resp.ThirdPartyBindings, []string{"telegram"}) || !slices.Equal(notified, []string{"telegram"}) {
		t.Fatalf("bindings = %v, notified = %v; want [telegram]", resp.ThirdPartyBindings, notified)
	}
	hash, algo, _ := f.storedPassword(t, owner)
	if !password.MultiPasswordVerify(algo, "", "new-password-1", hash) {
		t.Fatal("the new password was not stored")
	}
	rows := f.Logs(t, log.TypeLogin, owner.Id)
	if len(rows) != 1 {
		t.Fatalf("login audits = %d, want the password change", len(rows))
	}
	var audit log.Login
	if err := json.Unmarshal([]byte(rows[0].Content), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Method != account.PasswordChange || !audit.Success || audit.LoginIP != identitytest.ClientIP {
		t.Fatalf("audit = %+v", audit)
	}
}
