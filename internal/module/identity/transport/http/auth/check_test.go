package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The existence checks run over the real identity facade, with one account
// signing in with an email address and a phone number. They read the
// address or number from the query string of a GET, also when the client
// labels the bodyless GET as JSON; a GET whose JSON body does not parse is a
// parameter error.
func TestExistenceChecksReadTheQueryString(t *testing.T) {
	logtest.Discard(t)
	env := identitytest.New(t)
	enabled := true
	owner := &user.User{Enable: &enabled}
	if err := env.DB.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	for authType, identifier := range map[string]string{"email": "owner@example.com", "mobile": "+8613800138000"} {
		if err := env.DB.Create(&user.AuthMethods{UserId: owner.Id, AuthType: authType, AuthIdentifier: identifier, Verified: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	facade := identity.New(identity.Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
	})
	router := server.New()
	router.GET("/v1/auth/check", CheckUserHandler(facade))
	router.GET("/v1/auth/check/telephone", CheckUserTelephoneHandler(facade))

	for name, tc := range map[string]struct {
		target string
		// asJSON labels the request, which carries body, as JSON.
		asJSON bool
		body   string
		code   uint32
		// reply is the data of a success, or the message of a failure when
		// it is not the JSON decoder's own.
		reply string
	}{
		"known email":         {"/v1/auth/check?email=owner@example.com", false, "", xerr.SUCCESS, `{"exist":true}`},
		"known email as JSON": {"/v1/auth/check?email=owner@example.com", true, "", xerr.SUCCESS, `{"exist":true}`},
		"unknown email":       {"/v1/auth/check?email=nobody@example.com", false, "", xerr.SUCCESS, `{"exist":false}`},
		"no email":            {"/v1/auth/check", false, "", xerr.InvalidParams, "Email is a required field"},
		"invalid email":       {"/v1/auth/check?email=not-an-email", false, "", xerr.InvalidParams, "Email must be a valid email address"},
		"unparsable email":    {"/v1/auth/check?email=owner@example.com", true, `{"email":`, xerr.InvalidParams, ""},
		"known number":        {"/v1/auth/check/telephone?telephone=13800138000&telephone_area_code=86", false, "", xerr.SUCCESS, `{"exist":true}`},
		"unknown number":      {"/v1/auth/check/telephone?telephone=13900139000&telephone_area_code=86", false, "", xerr.SUCCESS, `{"exist":false}`},
		"number without area": {"/v1/auth/check/telephone?telephone=13800138000", false, "", xerr.InvalidParams, "TelephoneAreaCode is a required field"},
		"malformed number":    {"/v1/auth/check/telephone?telephone=abc&telephone_area_code=86", false, "", xerr.TelephoneError, "telephone number error"},
		"unparsable number":   {"/v1/auth/check/telephone?telephone=13800138000&telephone_area_code=86", true, `{"telephone":`, xerr.InvalidParams, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var headers []ut.Header
			if tc.asJSON {
				headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
			}
			var body *ut.Body
			if tc.body != "" {
				body = &ut.Body{Body: strings.NewReader(tc.body), Len: len(tc.body)}
			}
			w := ut.PerformRequest(router.Engine, http.MethodGet, tc.target, body, headers...)
			var got reply
			if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
				t.Fatalf("answer = %d %q (%v)", w.Code, w.Body, err)
			}
			if got.Code != tc.code || (tc.code == xerr.SUCCESS && string(got.Data) != tc.reply) || (tc.code != xerr.SUCCESS && tc.reply != "" && got.Msg != tc.reply) {
				t.Fatalf("reply = {%d %q %s}, want code %d with %s", got.Code, got.Msg, got.Data, tc.code, tc.reply)
			}
		})
	}
}
