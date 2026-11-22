package user

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	account "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// currentUserService answers with the administrator's view and records
// whom the handler asked for it.
type currentUserService struct {
	calls   int
	account *account.User
	err     error
}

var _ CurrentUserService = (*currentUserService)(nil)

func (s *currentUserService) CurrentUser(ctx context.Context) (*dto.User, error) {
	s.calls++
	s.account, _ = account.FromContext(ctx)
	if s.err != nil {
		return nil, s.err
	}
	return &dto.User{Id: s.account.Id, IsAdmin: true, Balance: 1200, ReferCode: "ADMIN"}, nil
}

// getCurrent asks for the current user as the signed-in administrator; a
// stand-in for the admin group's auth middleware puts the account in the
// request context.
func getCurrent(t *testing.T, admin *account.User, svc *currentUserService) (uint32, string, json.RawMessage) {
	t.Helper()
	logtest.Discard(t)
	h := server.New()
	h.Use(func(ctx context.Context, c *app.RequestContext) { c.Next(account.NewContext(ctx, admin)) })
	h.GET("/v1/admin/user/current", CurrentUserHandler(svc))
	w := ut.PerformRequest(h.Engine, http.MethodGet, "/v1/admin/user/current?id=99", nil)
	var reply struct {
		Code uint32          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); w.Code != http.StatusOK || err != nil {
		t.Fatalf("answer = %d %q (%v)", w.Code, w.Body, err)
	}
	return reply.Code, reply.Msg, reply.Data
}

// The admin panel's own account is the one the request context carries,
// whatever the query string names; the reply carries the facade's view of
// it.
func TestCurrentUserAnswersForTheSignedInAdministrator(t *testing.T) {
	isAdmin := true
	admin := &account.User{Id: 5, IsAdmin: &isAdmin}
	svc := &currentUserService{}
	code, msg, data := getCurrent(t, admin, svc)
	if svc.calls != 1 || svc.account != admin {
		t.Fatalf("calls = %d, account = %+v; want one call for the signed-in administrator", svc.calls, svc.account)
	}
	var got dto.User
	if err := json.Unmarshal(data, &got); err != nil || code != xerr.SUCCESS || msg != "success" {
		t.Fatalf("reply = {%d %q %s} (%v), want success", code, msg, data, err)
	}
	if want := (dto.User{Id: 5, IsAdmin: true, Balance: 1200, ReferCode: "ADMIN"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("data = %+v, want %+v", got, want)
	}
}

// A failure, such as a wallet that cannot be read, is reported in the
// envelope with the facade's code and no data.
func TestCurrentUserReportsAFacadeErrorInTheEnvelope(t *testing.T) {
	svc := &currentUserService{err: xerr.Errorf(xerr.DatabaseQueryError, "load user wallet error")}
	code, msg, data := getCurrent(t, &account.User{Id: 5}, svc)
	if code != xerr.DatabaseQueryError || msg != "Database query error" || data != nil {
		t.Fatalf("reply = {%d %q %s}, want the database query error without data", code, msg, data)
	}
}
