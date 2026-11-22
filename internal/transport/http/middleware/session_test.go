package middleware

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/auth/token"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const sessionTestSecret = "session-test-secret"

// sessionAccounts is an account table of enabled users; admins lists the
// administrators and devices the devices by id.
type sessionAccounts struct {
	admins  map[int64]bool
	devices map[int64]*user.Device
	missing error
}

var _ SessionAccounts = sessionAccounts{}

func (a sessionAccounts) FindUser(_ context.Context, id int64) (*user.User, error) {
	if a.missing != nil {
		return nil, a.missing
	}
	enabled, admin := true, a.admins[id]
	return &user.User{Id: id, Enable: &enabled, IsAdmin: &admin}, nil
}

func (a sessionAccounts) FindAccountStateForAuth(_ context.Context, id int64) (*user.AccountState, error) {
	if a.missing != nil {
		return nil, a.missing
	}
	enabled, admin := true, a.admins[id]
	return &user.AccountState{Id: id, Enable: &enabled, IsAdmin: &admin}, nil
}

func (a sessionAccounts) FindDeviceForAuth(_ context.Context, id int64) (*user.Device, error) {
	if device, ok := a.devices[id]; ok {
		return device, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func newSessionDeps(t *testing.T, accounts sessionAccounts) (AuthDeps, *redis.Client) {
	t.Helper()
	rds := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	return AuthDeps{JWT: config.JwtAuth{AccessSecret: sessionTestSecret}, Redis: rds, Accounts: accounts}, rds
}

func sessionToken(t *testing.T, rds *redis.Client, grant usersession.Grant) string {
	t.Helper()
	signed, err := usersession.Issue(context.Background(), rds, sessionTestSecret, 3600, grant)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// Order event tickets are signed with the same secret but are not sessions;
// they must be refused rather than crash the request.
func TestAuthenticateRequestRefusesTokensThatAreNotSessions(t *testing.T) {
	deps, _ := newSessionDeps(t, sessionAccounts{})
	ticket, err := token.NewJwtToken(sessionTestSecret, time.Now().Unix(), 3600,
		token.WithOption("OrderNo", "20260927000000"), token.WithOption("Scope", "order_events"), token.WithOption("UserId", int64(7)))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := AuthenticateRequest(context.Background(), deps, ticket); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("an order event ticket: error = %v, want InvalidAccess", err)
	}
	if _, err := AuthenticateRequest(context.Background(), deps, "garbage"); xerr.CodeOf(err) != xerr.ErrorTokenExpire {
		t.Fatalf("a garbage token: error = %v, want ErrorTokenExpire", err)
	}
}

func TestAuthenticateRequestRefusesRevokedSessions(t *testing.T) {
	deps, rds := newSessionDeps(t, sessionAccounts{})
	signed := sessionToken(t, rds, usersession.Grant{UserID: 7})
	ctx, err := AuthenticateRequest(context.Background(), deps, signed)
	if err != nil {
		t.Fatalf("live session refused: %v", err)
	}
	if u, ok := user.FromContext(ctx); !ok || u.Id != 7 {
		t.Fatalf("context user = %+v, %v", u, ok)
	}
	if sessionID, _ := ctx.Value(requestctx.CtxKeySessionID).(string); sessionID == "" {
		t.Fatal("the session id is missing from the context")
	}

	if err := usersession.Revoke(context.Background(), rds, 7); err != nil {
		t.Fatal(err)
	}

	if _, err := AuthenticateRequest(context.Background(), deps, signed); err == nil {
		t.Fatal("a revoked session still authenticates")
	}
	if _, err := AuthenticateRequest(context.Background(), deps, sessionToken(t, rds, usersession.Grant{UserID: 7})); err != nil {
		t.Fatalf("session issued after the revocation refused: %v", err)
	}
}

// A device session lives only while its device is enabled and still the
// user's.
func TestAuthenticateRequestChecksTheSessionDevice(t *testing.T) {
	accounts := sessionAccounts{devices: map[int64]*user.Device{
		9:  {Id: 9, UserId: 7, Enabled: true},
		10: {Id: 10, UserId: 7, Enabled: false},
		11: {Id: 11, UserId: 8, Enabled: true},
	}}
	deps, rds := newSessionDeps(t, accounts)
	for device, wantOK := range map[int64]bool{9: true, 10: false, 11: false, 12: false} {
		signed := sessionToken(t, rds, usersession.Grant{UserID: 7, LoginType: "device", DeviceID: device})
		_, err := AuthenticateRequest(context.Background(), deps, signed)
		if (err == nil) != wantOK {
			t.Errorf("device %d: error = %v, want ok = %v", device, err, wantOK)
		}
	}
}

func TestAuthenticateRequestReportsAccountLookupFailures(t *testing.T) {
	deps, rds := newSessionDeps(t, sessionAccounts{missing: errors.New("database down")})
	signed := sessionToken(t, rds, usersession.Grant{UserID: 7})
	if _, err := AuthenticateRequest(context.Background(), deps, signed); xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("error = %v, want DatabaseQueryError", err)
	}
}

// An authenticated session passes AdminGuard only for an administrator.
func TestRequireAdminAdmitsOnlyAdministrators(t *testing.T) {
	deps, rds := newSessionDeps(t, sessionAccounts{admins: map[int64]bool{1: true}})
	for userID, admitted := range map[int64]bool{7: false, 1: true} {
		ctx, err := AuthenticateRequest(context.Background(), deps, sessionToken(t, rds, usersession.Grant{UserID: userID}))
		if err != nil {
			t.Fatalf("user %d: AuthenticateRequest: %v", userID, err)
		}
		err = RequireAdmin(ctx)
		if admitted && err != nil {
			t.Fatalf("administrator %d was refused: %v", userID, err)
		}
		if !admitted && xerr.CodeOf(err) != xerr.InvalidAccess {
			t.Fatalf("user %d: RequireAdmin = %v, want InvalidAccess", userID, err)
		}
	}
	if err := RequireAdmin(context.Background()); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("anonymous: RequireAdmin = %v, want InvalidAccess", err)
	}
}

// A live session without the account lookups wired is refused, not
// dereferenced.
func TestAuthenticateRequestRefusesSessionsWithoutAccountLookups(t *testing.T) {
	deps, rds := newSessionDeps(t, sessionAccounts{})
	deps.Accounts = nil
	signed := sessionToken(t, rds, usersession.Grant{UserID: 7})
	if _, err := AuthenticateRequest(context.Background(), deps, signed); xerr.CodeOf(err) != xerr.ERROR {
		t.Fatalf("error = %v, want ERROR", err)
	}
}

// AdminGuard admits administrators and answers everyone else with
// InvalidAccess, whatever the path.
func TestAdminGuardAdmitsOnlyAdministrators(t *testing.T) {
	admin, member := true, false
	for name, tc := range map[string]struct {
		user    *user.User
		allowed bool
	}{
		"administrator": {&user.User{Id: 1, IsAdmin: &admin}, true},
		"member":        {&user.User{Id: 2, IsAdmin: &member}, false},
		"no flag":       {&user.User{Id: 3}, false},
		"anonymous":     {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			engine := server.Default()
			reached := false
			engine.GET("/v1/manage/users", func(ctx context.Context, c *app.RequestContext) {
				if tc.user != nil {
					ctx = user.NewContext(ctx, tc.user)
				}
				c.Next(ctx)
			}, AdminGuard(), func(_ context.Context, c *app.RequestContext) {
				reached = true
				c.String(http.StatusOK, "ok")
			})
			c := requestContext(engine, http.MethodGet, "/v1/manage/users")
			engine.ServeHTTP(context.Background(), c)
			if reached != tc.allowed {
				t.Fatalf("handler reached = %v, want %v", reached, tc.allowed)
			}
			if !tc.allowed {
				assertErrorEnvelope(t, c.Response.Body(), xerr.InvalidAccess, "Invalid access")
			}
		})
	}
}
