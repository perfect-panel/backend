package authn

import (
	"context"
	"fmt"
	"strings"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// fromIP is a request context from the client address ip.
func fromIP(ip string) context.Context {
	return requestmeta.With(context.Background(), requestmeta.New(ip, identitytest.UserAgent))
}

// device stores a device of an enabled account.
func (f *fixture) device(t *testing.T, identifier string, enabled bool) *user.User {
	t.Helper()
	active := true
	u := &user.User{Enable: &active}
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Create(&user.AuthMethods{UserId: u.Id, AuthType: "device", AuthIdentifier: identifier, Verified: true}).Error; err != nil {
		t.Fatal(err)
	}
	device := &user.Device{UserId: u.Id, Identifier: identifier, Enabled: true, Ip: "192.0.2.1"}
	if err := f.DB.Create(device).Error; err != nil {
		t.Fatal(err)
	}
	// The column defaults to true, which a zero-value insert would keep.
	if !enabled {
		if err := f.DB.Model(&user.Device{}).Where("id = ?", device.Id).Update("enabled", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	return u
}

// A device identifier is a bearer credential, so attempts with it are capped
// per identifier like a password: once the window's attempts on one
// identifier fail (registration closed, device disabled), further attempts
// with it are refused before the lookup, from any address, until the window
// ends. A successful sign-in clears the identifier's count, and the counter
// is keyed by the identifier's digest, never the identifier.
func TestDeviceLoginAttemptsAreCappedPerIdentifier(t *testing.T) {
	f := newFixture(t)
	f.cfg.StopRegister = true
	owner := f.device(t, "real-device-identifier", true)
	f.device(t, "disabled-device-identifier", false)

	for i := 0; i < account.MaxDeviceLoginAttempts; i++ {
		_, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "guessed-device-identifier"})
		assertCode(t, err, xerr.StopRegister)
	}
	_, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "guessed-device-identifier"})
	assertCode(t, err, xerr.TooManyRequests)
	for _, key := range f.Mini.Keys() {
		if strings.HasPrefix(key, "auth:device_login_attempts:") && strings.Contains(key, "device-identifier") {
			t.Fatalf("key %q names the identifier", key)
		}
	}

	// The real device signs in, which clears its count.
	resp, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "real-device-identifier"})
	if err != nil || f.sessionUser(t, resp.Token) != owner.Id {
		t.Fatalf("the real device's sign-in = %v", err)
	}
	if f.Mini.Exists(account.DeviceLoginAttemptKey("real-device-identifier")) {
		t.Fatal("a successful sign-in did not clear the identifier's attempts")
	}

	// A disabled device hammered from many addresses locks its identifier.
	for i := 0; i < account.MaxDeviceLoginAttempts; i++ {
		_, err := f.svc.DeviceLogin(fromIP(fmt.Sprintf("198.51.100.%d", i+1)), &dto.DeviceLoginRequest{Identifier: "disabled-device-identifier"})
		assertCode(t, err, xerr.InvalidAccess)
	}
	_, err = f.svc.DeviceLogin(fromIP("198.51.100.200"), &dto.DeviceLoginRequest{Identifier: "disabled-device-identifier"})
	assertCode(t, err, xerr.TooManyRequests)
	f.Mini.FastForward(account.DeviceLoginAttemptWindow)
	_, err = f.svc.DeviceLogin(fromIP("198.51.100.200"), &dto.DeviceLoginRequest{Identifier: "disabled-device-identifier"})
	assertCode(t, err, xerr.InvalidAccess)
}

// One client address gets a bounded number of device sign-ins an hour,
// whatever identifiers it tries, so identifiers cannot be enumerated at line
// speed; another address keeps its own quota.
func TestDeviceLoginAttemptsAreCappedPerClientAddress(t *testing.T) {
	f := newFixture(t)
	f.cfg.StopRegister = true
	f.device(t, "real-device-identifier", true)
	attacker := fromIP("198.51.100.7")

	for i := 0; i < account.MaxDeviceLoginIPAttempts; i++ {
		_, err := f.svc.DeviceLogin(attacker, &dto.DeviceLoginRequest{Identifier: fmt.Sprintf("guess-%d", i)})
		assertCode(t, err, xerr.StopRegister)
	}
	_, err := f.svc.DeviceLogin(attacker, &dto.DeviceLoginRequest{Identifier: "real-device-identifier"})
	assertCode(t, err, xerr.TooManyRequests)
	if _, err := f.svc.DeviceLogin(fromIP("203.0.113.9"), &dto.DeviceLoginRequest{Identifier: "real-device-identifier"}); err != nil {
		t.Fatalf("the real device from another address: %v", err)
	}
	f.Mini.FastForward(account.DeviceLoginIPAttemptWindow)
	if _, err := f.svc.DeviceLogin(attacker, &dto.DeviceLoginRequest{Identifier: "real-device-identifier"}); err != nil {
		t.Fatalf("sign-in after the window: %v", err)
	}
}

// With the sign-in challenge on, a device sign-in needs a valid Turnstile
// token, for a known device and for a first-time one alike; the first-time
// device is not asked a second time for its registration, since a token is
// redeemed once.
func TestDeviceLoginAppliesTheSignInChallenge(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	cfg := openSnapshot()
	cfg.LoginVerify, cfg.RegisterVerify, cfg.TurnstileSecret = true, true, "site-secret"
	var asked []string
	svc := NewService(Deps{
		Store: env.Store, Redis: env.Redis, Config: func() Snapshot { return cfg },
		VerifyTurnstile: func(_ context.Context, _, token, _ string) (bool, error) {
			asked = append(asked, token)
			return token == "human", nil
		},
	})
	f := &fixture{Env: env, svc: svc, cfg: &cfg}
	f.device(t, "known-device-identifier", true)

	_, err := svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "known-device-identifier"})
	assertCode(t, err, xerr.TooManyRequests)
	_, err = svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "known-device-identifier", CfToken: "robot"})
	assertCode(t, err, xerr.TooManyRequests)
	if _, err := svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "known-device-identifier", CfToken: "human"}); err != nil {
		t.Fatalf("a human's sign-in: %v", err)
	}
	asked = nil
	if _, err := svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "first-time-device-identifier", CfToken: "human"}); err != nil {
		t.Fatalf("a human's first sign-in: %v", err)
	}
	if len(asked) != 1 {
		t.Fatalf("the challenge was checked %d times for one registration, want once", len(asked))
	}
	if n := len(f.Users(t)); n != 2 {
		t.Fatalf("accounts = %d, want the known device's and the new one", n)
	}
}

// The identifier signs the device in, so the log of a registration names
// the account and device rows, not the identifier.
func TestDeviceRegistrationLogsNoIdentifier(t *testing.T) {
	f := newFixture(t)
	logs := logtest.NewCollector(t)
	if _, err := f.svc.DeviceLogin(identitytest.Context(), &dto.DeviceLoginRequest{Identifier: "secret-device-identifier-0001"}); err != nil {
		t.Fatalf("DeviceLogin() error = %v", err)
	}
	if entries := logs.String(); strings.Contains(entries, "secret-device-identifier-0001") {
		t.Fatalf("logs = %q, want the identifier kept out", entries)
	}
}
