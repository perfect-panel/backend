package identity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	dto "github.com/perfect-panel/server/internal/module/identity/contract"
	"github.com/perfect-panel/server/internal/module/identity/entity/auth"
	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/server/pkg/logger/logtest"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"gorm.io/gorm"
)

// The facade serves the entry points from the module's own repositories:
// the bootstrap's administrator and auth-method reads, the server start's
// check and fix-up, the device socket's presence and the session lookups.
func TestFacadeServesTheEntryPoints(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	svc := New(Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
	})
	ctx := context.Background()

	if created, err := svc.CreateInitialAdministrator(ctx, "admin@example.com", "first-secret"); err != nil || !created {
		t.Fatalf("CreateInitialAdministrator() = %t, %v", created, err)
	}
	admins, err := svc.FindAdministratorsWithPassword(ctx, "first-secret")
	if err != nil || len(admins) != 1 {
		t.Fatalf("FindAdministratorsWithPassword() = %+v, %v, want the seeded administrator", admins, err)
	}
	if err := svc.ValidateEmailIdentities(ctx); err != nil {
		t.Fatalf("ValidateEmailIdentities() = %v", err)
	}
	if err := svc.NormalizePhoneNumbers(ctx); err != nil {
		t.Fatalf("NormalizePhoneNumbers() = %v", err)
	}

	admin := admins[0]
	device := &user.Device{UserId: admin.Id, Identifier: "admin-phone", Ip: "192.0.2.1"}
	if err := env.DB.Create(device).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkDeviceOnline(ctx, device.Identifier); err != nil {
		t.Fatalf("MarkDeviceOnline() = %v", err)
	}
	if current, err := svc.FindDeviceForAuth(ctx, device.Id); err != nil || current.UserId != admin.Id || !current.Enabled {
		t.Fatalf("FindDeviceForAuth() = %+v, %v, want the administrator's enabled device", current, err)
	}
	if err := svc.MarkDeviceOffline(ctx, admin.Id, device.Identifier, timeutil.Now()); err != nil {
		t.Fatalf("MarkDeviceOffline() = %v", err)
	}
	var records int64
	if err := env.DB.Model(&user.DeviceOnlineRecord{}).Where("identifier = ?", device.Identifier).Count(&records).Error; err != nil || records != 1 {
		t.Fatalf("online records = %d, %v, want the connection recorded", records, err)
	}
	if found, err := svc.FindUser(ctx, admin.Id); err != nil || found.IsAdmin == nil || !*found.IsAdmin {
		t.Fatalf("FindUser() = %+v, %v, want the administrator", found, err)
	}

	env.EnableMethod(t, "email", `{"platform":"smtp"}`)
	if method, err := svc.FindLoginMethod(ctx, "email"); err != nil || method.Config != `{"platform":"smtp"}` {
		t.Fatalf("FindLoginMethod(email) = %+v, %v, want the stored configuration", method, err)
	}
	if _, err := svc.FindLoginMethod(ctx, "telegram"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("FindLoginMethod(telegram) error = %v, want gorm.ErrRecordNotFound", err)
	}
}

// codeQueue keeps the verification-code deliveries the facade enqueues.
type codeQueue struct{ tasks []*asynq.Task }

func (q *codeQueue) EnqueueContext(_ context.Context, task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "task"}, nil
}

// Closing registration keeps members able to bind or change their email: a
// signed-in account gets the code for an address no account holds and binds
// it, while anonymous callers can neither get a registration code nor
// register.
func TestMembersBindAnEmailWhileRegistrationIsClosed(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	queue := &codeQueue{}
	svc := New(Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
		AuthConfig: func() AuthSnapshot {
			return AuthSnapshot{JWTAccessSecret: "secret", JWTAccessExpire: 3600, EmailEnabled: true, StopRegister: true}
		},
		VerifyQueue:      queue,
		VerifyCodeConfig: func() VerifyCodeSnapshot { return VerifyCodeSnapshot{VerifyCodeExpire: 300} },
		EmailDomains:     func() (string, bool) { return "", false },
	})
	enabled := true
	member := &user.User{Enable: &enabled}
	if err := env.DB.Create(member).Error; err != nil {
		t.Fatal(err)
	}
	anonymous := identitytest.Context()
	signedIn := user.NewContext(anonymous, member)

	_, err := svc.SendEmailCode(anonymous, &dto.SendCodeRequest{Email: "member@example.com", Type: uint8(auth.Register)})
	if code := xerr.CodeOf(err); code != xerr.StopRegister {
		t.Fatalf("anonymous SendEmailCode() error = %v, want StopRegister", err)
	}
	if _, err := svc.SendEmailCode(signedIn, &dto.SendCodeRequest{Email: "member@example.com", Type: uint8(auth.Register)}); err != nil {
		t.Fatalf("SendEmailCode() for the member: %v", err)
	}
	if len(queue.tasks) != 1 {
		t.Fatalf("deliveries = %d, want the member's code", len(queue.tasks))
	}
	var payload taskqueue.SendEmailPayload
	if err := json.Unmarshal(queue.tasks[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	code, _ := payload.Content["Code"].(string)

	_, err = svc.UserRegister(anonymous, &dto.UserRegisterRequest{Email: "member@example.com", Password: "password-1", Code: code})
	if xerr.CodeOf(err) != xerr.StopRegister {
		t.Fatalf("UserRegister() error = %v, want StopRegister", err)
	}
	if err := svc.UpdateBindEmail(signedIn, &dto.UpdateBindEmailRequest{Email: "member@example.com", Code: code}); err != nil {
		t.Fatalf("UpdateBindEmail() error = %v", err)
	}
	identities := env.Identities(t, member.Id)
	if len(identities) != 1 || identities[0].AuthType != "email" || identities[0].AuthIdentifier != "member@example.com" || !identities[0].Verified {
		t.Fatalf("identities = %+v, want the verified email binding", identities)
	}
	if n := len(env.Users(t)); n != 1 {
		t.Fatalf("accounts = %d, want only the member", n)
	}
}
