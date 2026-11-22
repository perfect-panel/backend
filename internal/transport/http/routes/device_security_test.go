package routes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/perfect-panel/backend/internal/auth/deviceauth"
	"github.com/perfect-panel/backend/internal/auth/devicesession"
	"github.com/perfect-panel/backend/internal/auth/password"
	authtoken "github.com/perfect-panel/backend/internal/auth/token"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	authhttp "github.com/perfect-panel/backend/internal/module/identity/transport/http/auth"
	"github.com/perfect-panel/backend/internal/module/platform"
	logentity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/outbox"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/internal/transport/http/middleware"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

const deviceTestJWTKey = "device-test-only-signing-key"

type deviceFixture struct {
	db           *gorm.DB
	store        *repository.GormStore
	rdb          *miniredis.Miniredis
	redis        *redis.Client
	owner, other *user.User
	devices      []*user.Device
}

func newDeviceFixture(t *testing.T) *deviceFixture {
	t.Helper()
	logtest.Discard(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true, IgnoreRelationshipsWhenMigrating: true, Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&user.User{}, &user.AuthMethods{}); err != nil {
		t.Fatal(err)
	}
	// SQLite index names are database-global, unlike the MySQL entity tags.
	if err := db.Migrator().RenameIndex(&user.AuthMethods{}, "idx_user_id", "idx_auth_methods_user_id"); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&user.Device{}, &logentity.SystemLog{}, &outbox.Event{}); err != nil {
		t.Fatal(err)
	}
	rdb := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: rdb.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rds.Close() })
	store := repository.NewGormStoreWithBuilders(db, rds, repository.Builders{
		Identity: identity.NewRepoBuilder(), Platform: platform.NewRepoBuilder(),
		Billing: func(repository.ModuleConn) repository.BillingRepos { return repository.BillingRepos{} },
		Network: func(repository.ModuleConn) repository.NetworkRepos { return repository.NetworkRepos{} },
		Subscription: func(repository.ModuleConn, repository.NodeCacheKeyBridge) repository.SubscriptionRepos {
			return repository.SubscriptionRepos{}
		},
		Support:      func(repository.ModuleConn) repository.SupportRepos { return repository.SupportRepos{} },
		Notification: func(repository.ModuleConn) repository.NotificationRepos { return repository.NotificationRepos{} },
	})
	enabled := true
	f := &deviceFixture{db: db, store: store, rdb: rdb, redis: rds, owner: &user.User{Enable: &enabled}, other: &user.User{Enable: &enabled, Password: password.EncodePassWord("test-password"), Algo: password.PasswordAlgoArgon2id}}
	for _, u := range []*user.User{f.owner, f.other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&user.AuthMethods{UserId: f.other.Id, AuthType: "email", AuthIdentifier: "other@example.test", Verified: true}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		d := &user.Device{UserId: f.owner.Id, Identifier: fmt.Sprintf("test-device-%d", i), Enabled: true, Ip: "old-ip", UserAgent: "old-ua"}
		if err := db.Create(d).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&user.AuthMethods{UserId: f.owner.Id, AuthType: "device", AuthIdentifier: d.Identifier, Verified: true}).Error; err != nil {
			t.Fatal(err)
		}
		f.devices = append(f.devices, d)
	}
	return f
}

func (f *deviceFixture) service(kick func(int64, string), onlyReal bool) identity.Service {
	return identity.New(identity.Deps{
		Store: f.store, Redis: f.redis, Users: f.store.User(), UserAuths: f.store.UserAuth(),
		Devices: f.store.UserDevice(), Cache: f.store.UserCache(), Logs: f.store.Log(),
		Auths: f.store.Auth(), KickDevice: kick,
		AuthConfig: func() identity.AuthSnapshot {
			return identity.AuthSnapshot{DeviceEnabled: true, DeviceOnlyReal: onlyReal, EmailEnabled: true,
				JWTAccessSecret: deviceTestJWTKey, JWTAccessExpire: 3600}
		},
	})
}

func (f *deviceFixture) login(identifier string) (*dto.LoginResponse, error) {
	ctx := requestmeta.With(context.Background(), requestmeta.New("192.0.2.99", "original-test-UA"))
	return f.service(nil, false).DeviceLogin(ctx, &dto.DeviceLoginRequest{Identifier: identifier})
}

func (f *deviceFixture) token(t *testing.T, index int) string {
	t.Helper()
	resp, err := f.login(f.devices[index].Identifier)
	if err != nil {
		t.Fatal(err)
	}
	return resp.Token
}

func (f *deviceFixture) authorize(token string) (context.Context, error) {
	return middleware.AuthenticateRequest(context.Background(), middleware.AuthDeps{JWT: config.JwtAuth{AccessSecret: deviceTestJWTKey}, Redis: f.redis, Accounts: f.service(nil, false)}, token)
}

func (f *deviceFixture) admin(kick func(int64, string)) identity.Service {
	return f.service(kick, false)
}

func TestDeviceLoginUsesCurrentStateAndRecordsBinding(t *testing.T) {
	f := newDeviceFixture(t)
	token := f.token(t, 0)
	claims, err := authtoken.ParseJwtToken(token, deviceTestJWTKey)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := devicesession.Binding(claims)
	if err != nil || id != f.devices[0].Id {
		t.Fatalf("missing binding: %v", err)
	}
	if _, err := f.authorize(token); err != nil {
		t.Fatal(err)
	}
	var device user.Device
	if err := f.db.First(&device, f.devices[0].Id).Error; err != nil {
		t.Fatal(err)
	}
	if device.Ip != "192.0.2.99" || device.UserAgent != "original-test-UA" {
		t.Fatalf("metadata not refreshed: %+v", device)
	}
	// Prime the legacy lookup cache, then change authoritative state directly.
	if _, err := f.store.UserDevice().FindOneDeviceByIdentifier(context.Background(), device.Identifier); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&user.Device{}).Where("id = ?", device.Id).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.login(device.Identifier); err == nil {
		t.Fatal("disabled device logged in through stale cache")
	}
	if _, err := f.authorize(token); err == nil {
		t.Fatal("disabled device session remained valid")
	}
}

func TestDeviceRegistrationPreservesAccountAndCreatesBoundSession(t *testing.T) {
	f := newDeviceFixture(t)
	resp, err := f.login("new-device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.authorize(resp.Token); err != nil {
		t.Fatal(err)
	}
	var count int64
	f.db.Model(&outbox.Event{}).Where("topic = ?", "identity.user_registered").Count(&count)
	if count != 1 {
		t.Fatalf("registration events = %d", count)
	}
	var d user.Device
	if err := f.db.Where("identifier = ?", "new-device").First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.UserId == f.owner.Id || !d.Enabled {
		t.Fatal("registration did not create an enabled independent account")
	}
}

func TestEmailLoginCannotStealAnotherAccountsDevice(t *testing.T) {
	f := newDeviceFixture(t)
	if _, err := f.service(nil, false).UserLogin(context.Background(), &dto.UserLoginRequest{Email: "other@example.test", Password: "test-password", Identifier: f.devices[0].Identifier}); err == nil {
		t.Fatal("foreign binding was accepted")
	}
	var owner user.User
	f.db.First(&owner, f.owner.Id)
	var d user.Device
	f.db.First(&d, f.devices[0].Id)
	var auth user.AuthMethods
	f.db.Where("auth_type = ? AND auth_identifier = ?", "device", d.Identifier).First(&auth)
	if !*owner.Enable || d.UserId != owner.Id || auth.UserId != owner.Id {
		t.Fatal("foreign account or device was changed")
	}
	for _, key := range f.rdb.Keys() {
		if strings.HasPrefix(key, config.SessionIdKey+":") {
			t.Fatal("login issued a session despite failed binding")
		}
	}
}

func TestDeviceDisableEnableAndOfflineKickRevokeOnlyTarget(t *testing.T) {
	f := newDeviceFixture(t)
	first, other := f.token(t, 0), f.token(t, 1)
	kicks := 0
	admin := f.admin(func(uid int64, id string) {
		if uid != f.owner.Id || id != f.devices[0].Identifier {
			t.Error("wrong kick target")
		}
		kicks++
	})
	if err := admin.UpdateUserDevice(context.Background(), &dto.UserDevice{Id: f.devices[0].Id, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.login(f.devices[0].Identifier); err == nil {
		t.Fatal("disabled device logged in")
	}
	if _, err := f.authorize(first); err == nil {
		t.Fatal("disabled token valid")
	}
	if _, err := f.authorize(other); err != nil {
		t.Fatal("other device revoked", err)
	}
	if err := admin.UpdateUserDevice(context.Background(), &dto.UserDevice{Id: f.devices[0].Id, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.authorize(first); err == nil {
		t.Fatal("old token revived after re-enable")
	}
	newToken := f.token(t, 0)
	if err := admin.KickOfflineByUserDevice(context.Background(), &dto.KickOfflineRequest{Id: f.devices[0].Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.authorize(newToken); err == nil {
		t.Fatal("offline device token survived kick")
	}
	if _, err := f.authorize(other); err != nil {
		t.Fatal(err)
	}
	if kicks != 2 {
		t.Fatalf("kick callbacks = %d", kicks)
	}
}

func TestUnbindRevokesTargetNotCallingDeviceAndCleansBothRows(t *testing.T) {
	f := newDeviceFixture(t)
	target, caller := f.token(t, 0), f.token(t, 1)
	ctx, err := f.authorize(caller)
	if err != nil {
		t.Fatal(err)
	}
	kicks := 0
	svc := f.service(func(int64, string) { kicks++ }, false)
	if err := svc.UnbindDevice(ctx, &dto.UnbindDeviceRequest{Id: f.devices[0].Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.authorize(target); err == nil {
		t.Fatal("target session survived unbind")
	}
	if _, err := f.authorize(caller); err != nil {
		t.Fatal("caller was logged out", err)
	}
	var count int64
	f.db.Model(&user.Device{}).Where("id = ?", f.devices[0].Id).Count(&count)
	if count != 0 {
		t.Fatal("device row retained")
	}
	f.db.Model(&user.AuthMethods{}).Where("auth_type = ? AND auth_identifier = ?", "device", f.devices[0].Identifier).Count(&count)
	if count != 0 {
		t.Fatal("auth row retained")
	}
	if kicks != 1 {
		t.Fatal("active device was not disconnected")
	}
	// Removing via admin must use the same atomic cleanup, not leave a stale
	// unique auth identifier that prevents the next registration.
	if err := f.admin(nil).DeleteUserDevice(context.Background(), &dto.DeleteUserDeviceRequest{Id: f.devices[1].Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.authorize(caller); err == nil {
		t.Fatal("admin deletion left session valid")
	}
	if _, err := f.login(f.devices[1].Identifier); err != nil {
		t.Fatal("stale auth row blocks registration", err)
	}
}

func TestDeviceRemovalRollsBackRowsAndFailsClosedOnRedisError(t *testing.T) {
	f := newDeviceFixture(t)
	token := f.token(t, 0)
	ctx := context.Background()
	if err := f.service(nil, false).UnbindDevice(context.WithValue(ctx, requestctx.CtxKeyUser, f.other), &dto.UnbindDeviceRequest{Id: f.devices[0].Id}); err == nil {
		t.Fatal("cross-user removal accepted")
	}
	f.rdb.SetError("unavailable")
	if err := f.admin(nil).DeleteUserDevice(ctx, &dto.DeleteUserDeviceRequest{Id: f.devices[0].Id}); err == nil {
		t.Fatal("Redis failure was ignored")
	}
	f.rdb.SetError("")
	if _, err := f.authorize(token); err != nil {
		t.Fatal("failed removal changed session", err)
	}
	if err := f.db.Exec(`CREATE TRIGGER reject_device_auth_delete BEFORE DELETE ON user_auth_methods BEGIN SELECT RAISE(FAIL, 'test failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.admin(nil).DeleteUserDevice(ctx, &dto.DeleteUserDeviceRequest{Id: f.devices[0].Id}); err == nil {
		t.Fatal("database failure ignored")
	}
	var count int64
	f.db.Model(&user.Device{}).Where("id = ?", f.devices[0].Id).Count(&count)
	if count != 1 {
		t.Fatal("device deletion not rolled back")
	}
	f.db.Model(&user.AuthMethods{}).Where("auth_identifier = ?", f.devices[0].Identifier).Count(&count)
	if count != 1 {
		t.Fatal("auth deletion not rolled back")
	}
	if _, err := f.authorize(token); err == nil {
		t.Fatal("rollback must not revive a revoked token")
	}
}

func TestLegacyUnboundDeviceJWTRequiresNewLogin(t *testing.T) {
	f := newDeviceFixture(t)
	for _, loginType := range []string{"device", ""} {
		session := "legacy-" + loginType
		if err := f.rdb.Set(config.SessionIdKey+":"+session, fmt.Sprint(f.owner.Id)); err != nil {
			t.Fatal(err)
		}
		token, err := authtoken.NewJwtToken(deviceTestJWTKey, time.Now().Unix(), 3600, authtoken.WithOption("UserId", f.owner.Id), authtoken.WithOption("SessionId", session), authtoken.WithOption("LoginType", loginType))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.authorize(token)
		if (err != nil) != (loginType == "device") {
			t.Fatalf("legacy %q authorization: %v", loginType, err)
		}
	}
}

// peerConn is a connection with a fixed peer address, so a request served
// without a listener has a real address for Hertz to resolve the client IP
// from, as in Hertz's own tests. The embedded mock connection implements the
// rest of network.Conn.
type peerConn struct {
	*mock.Conn
	peer net.Addr
}

func (c peerConn) RemoteAddr() net.Addr { return c.peer }

// The device login records the raw HTTP User-Agent and the client IP the
// framework resolved, never the user_agent and IP fields a client puts in
// the body. The refreshed device row and the login audit show what reached
// the identity service.
func TestDeviceLoginHandlerUsesRawHTTPUserAgent(t *testing.T) {
	f := newDeviceFixture(t)
	router := server.New()
	router.Use(middleware.LoggerMiddleware())
	router.POST("/v1/auth/login/device", authhttp.DeviceLoginHandler(f.service(nil, false)))
	c := router.NewContext()
	c.SetConn(peerConn{Conn: mock.NewConn(""), peer: &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 41000}})
	c.Request.Header.SetMethod("POST")
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "Original-UA/1.0")
	c.Request.SetRequestURI("/v1/auth/login/device")
	body, _ := json.Marshal(map[string]string{"identifier": f.devices[0].Identifier, "user_agent": "spoofed", "IP": "forged"})
	c.Request.SetBody(body)
	router.ServeHTTP(context.Background(), c)
	var envelope map[string]any
	if err := json.Unmarshal(c.Response.Body(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["code"] != float64(200) {
		t.Fatalf("handler failed: %s", c.Response.Body())
	}
	clientIP := c.ClientIP()
	if clientIP != "203.0.113.7" {
		t.Fatalf("resolved client IP = %q, want the peer address", clientIP)
	}

	var device user.Device
	if err := f.db.First(&device, f.devices[0].Id).Error; err != nil {
		t.Fatal(err)
	}
	if device.UserAgent != "Original-UA/1.0" || device.Ip != clientIP {
		t.Fatalf("device refreshed with UA %q, IP %q; want %q, %q", device.UserAgent, device.Ip, "Original-UA/1.0", clientIP)
	}
	var row logentity.SystemLog
	if err := f.db.Where("type = ?", logentity.TypeLogin.Uint8()).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var audit logentity.Login
	if err := json.Unmarshal([]byte(row.Content), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.UserAgent != "Original-UA/1.0" || audit.LoginIP != clientIP {
		t.Fatalf("login audited with UA %q, IP %q; want %q, %q", audit.UserAgent, audit.LoginIP, "Original-UA/1.0", clientIP)
	}
}

func TestSignedDeviceLoginEndToEndRejectsReplay(t *testing.T) {
	f := newDeviceFixture(t)
	const path = "/v1/auth/login/device"
	const secret = "test-device-transport"
	router := server.New()
	router.Use(middleware.LoggerMiddleware())
	router.POST(path, middleware.DeviceMiddleware(func() config.DeviceConfig {
		return config.DeviceConfig{Enable: true, OnlyRealDevice: true, EnableSecurity: true, SecuritySecret: secret}
	}, f.redis), authhttp.DeviceLoginHandler(f.service(nil, true)))
	plain, _ := json.Marshal(map[string]string{"identifier": f.devices[0].Identifier, "user_agent": "spoofed"})
	data, timestamp, err := deviceauth.Encrypt(plain, secret)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(deviceauth.Envelope{Data: data, Time: timestamp, Sign: deviceauth.Sign(secret, "POST", path, "body", data, timestamp)})
	for i := 0; i < 2; i++ {
		c := router.NewContext()
		c.Request.Header.SetMethod("POST")
		c.Request.Header.Set("User-Agent", "Device-UA/1.0")
		c.Request.SetRequestURI(path)
		c.Request.SetBody(body)
		router.ServeHTTP(context.Background(), c)
		var response struct {
			Code int
			Data deviceauth.Envelope
		}
		if err := json.Unmarshal(c.Response.Body(), &response); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if response.Code == 200 {
				t.Fatal("replayed login succeeded")
			}
			continue
		}
		if response.Code != 200 {
			t.Fatalf("signed login failed: %s", c.Response.Body())
		}
		decoded, err := response.Data.Open(secret, "POST", path, "response", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var login dto.LoginResponse
		if err := json.Unmarshal([]byte(decoded), &login); err != nil {
			t.Fatal(err)
		}
		if _, err := f.authorize(login.Token); err != nil {
			t.Fatal(err)
		}
		var row logentity.SystemLog
		if err := f.db.Where("type = ?", logentity.TypeLogin.Uint8()).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		var audit logentity.Login
		if err := json.Unmarshal([]byte(row.Content), &audit); err != nil {
			t.Fatal(err)
		}
		if audit.UserAgent != "Device-UA/1.0" {
			t.Fatalf("audit UA = %q", audit.UserAgent)
		}
	}
}

func TestDeviceMetadataUpdatesCannotReenableOrReassign(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := context.Background()
	id := f.devices[0].Id
	if ok, err := f.store.UserDevice().TouchDevice(ctx, id, f.other.Id, "wrong-ip", "wrong-ua"); err != nil || ok {
		t.Fatalf("foreign touch = %v, %v", ok, err)
	}
	if err := f.store.UserDevice().SetDeviceEnabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UserDevice().SetDeviceOnline(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.store.UserDevice().TouchDevice(ctx, id, f.owner.Id, "wrong-ip", "wrong-ua"); err != nil || ok {
		t.Fatalf("disabled touch = %v, %v", ok, err)
	}
	var device user.Device
	if err := f.db.First(&device, id).Error; err != nil {
		t.Fatal(err)
	}
	if device.Enabled || device.Online || device.UserId != f.owner.Id || device.Ip != "old-ip" {
		t.Fatalf("device state overwritten: %+v", device)
	}
}

func TestGenericAuthEndpointsCannotMutateDeviceIdentity(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := context.WithValue(context.Background(), requestctx.CtxKeyUser, f.owner)
	admin := f.admin(nil)
	profileSvc := f.service(nil, false)
	for _, kind := range []string{"device", "DEVICE", "Device "} {
		if err := admin.CreateUserAuthMethod(ctx, &dto.CreateUserAuthMethodRequest{UserId: f.owner.Id, AuthType: kind}); err == nil {
			t.Fatal("generic device create accepted")
		}
		if err := admin.UpdateUserAuthMethod(ctx, &dto.UpdateUserAuthMethodRequest{UserId: f.owner.Id, AuthType: kind}); err == nil {
			t.Fatal("generic device update accepted")
		}
		if err := admin.DeleteUserAuthMethod(ctx, &dto.DeleteUserAuthMethodRequest{UserId: f.owner.Id, AuthType: kind}); err == nil {
			t.Fatal("generic device delete accepted")
		}
		if err := profileSvc.UnbindOAuth(ctx, &dto.UnbindOAuthRequest{Method: kind}); err == nil {
			t.Fatal("OAuth device removal accepted")
		}
	}
}
