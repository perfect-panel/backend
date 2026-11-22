package common

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/transport/http/internal/handlertest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// fakePublicInfo is the public-info port: it records every call and answers
// the recorder's answer for the method.
type fakePublicInfo struct{ handlertest.Recorder }

var _ PublicInfo = (*fakePublicInfo)(nil)

func (f *fakePublicInfo) GetGlobalConfig(context.Context) (*dto.GetGlobalConfigResponse, error) {
	return handlertest.Answer[dto.GetGlobalConfigResponse](&f.Recorder, "GetGlobalConfig", nil)
}

func (f *fakePublicInfo) GetTos(context.Context) (*dto.GetTosResponse, error) {
	return handlertest.Answer[dto.GetTosResponse](&f.Recorder, "GetTos", nil)
}

func (f *fakePublicInfo) GetPrivacyPolicy(context.Context) (*dto.PrivacyPolicyConfig, error) {
	return handlertest.Answer[dto.PrivacyPolicyConfig](&f.Recorder, "GetPrivacyPolicy", nil)
}

func (f *fakePublicInfo) GetStat(context.Context) (*dto.GetStatResponse, error) {
	return handlertest.Answer[dto.GetStatResponse](&f.Recorder, "GetStat", nil)
}

func (f *fakePublicInfo) GetClient(context.Context) (*dto.GetSubscribeClientResponse, error) {
	return handlertest.Answer[dto.GetSubscribeClientResponse](&f.Recorder, "GetClient", nil)
}

func (f *fakePublicInfo) Heartbeat(context.Context) (*dto.HeartbeatResponse, error) {
	return handlertest.Answer[dto.HeartbeatResponse](&f.Recorder, "Heartbeat", nil)
}

// commonRoutes registers the site-level handlers on their production routes.
func commonRoutes(port PublicInfo) *server.Hertz {
	h := server.New()
	group := h.Group("/v1/common")
	group.GET("/client", GetClientHandler(port))
	group.GET("/heartbeat", HeartbeatHandler(port))
	group.GET("/site/config", GetGlobalConfigHandler(port))
	group.GET("/site/privacy", GetPrivacyPolicyHandler(port))
	group.GET("/site/stat", GetStatHandler(port))
	group.GET("/site/tos", GetTosHandler(port))
	return h
}

var publicCases = []struct {
	target, call string
	answer       any
}{
	{"/v1/common/client", "GetClient", &dto.GetSubscribeClientResponse{Total: 1, List: []dto.SubscribeClient{{
		Id: 1, Name: "Clash", Scheme: "clash://install-config?url=", IsDefault: true,
		DownloadLink: dto.PlatformDownloadLinkSnapshot{Windows: "https://dl.example/clash.exe"},
	}}}},
	{"/v1/common/heartbeat", "Heartbeat", &dto.HeartbeatResponse{Status: true, Message: "service is alive", Timestamp: 1758000000}},
	{"/v1/common/site/config", "GetGlobalConfig", &dto.GetGlobalConfigResponse{
		Site:         dto.SiteConfig{Host: "panel.example", SiteName: "Panel"},
		Auth:         dto.AuthConfig{Email: dto.EmailAuthticateConfig{Enable: true, EnableVerify: true}},
		Currency:     dto.Currency{CurrencyUnit: "USD", CurrencySymbol: "$"},
		VerifyCode:   dto.PubilcVerifyCodeConfig{VerifyCodeInterval: 60},
		OAuthMethods: []string{"google"},
		WebAd:        true,
	}},
	{"/v1/common/site/privacy", "GetPrivacyPolicy", &dto.PrivacyPolicyConfig{PrivacyPolicy: "# Privacy"}},
	{"/v1/common/site/stat", "GetStat", &dto.GetStatResponse{User: 120, Node: 8, Country: 5, Protocol: []string{"vless", "hysteria2"}}},
	{"/v1/common/site/tos", "GetTos", &dto.GetTosResponse{TosContent: "# Terms"}},
}

// Each site-level route asks the facade once and answers its view as the
// envelope's data; none of them reads the request.
func TestPublicHandlersAnswerTheSiteViews(t *testing.T) {
	for _, tc := range publicCases {
		t.Run(tc.call, func(t *testing.T) {
			port := &fakePublicInfo{Recorder: handlertest.Recorder{Answers: map[string]any{tc.call: tc.answer}}}
			reply := handlertest.Serve(t, commonRoutes(port), http.MethodGet, tc.target+"?page=x", "")
			port.Called(t, tc.call, nil)
			reply.OK(t, tc.answer)
		})
	}
}

// A view the facade could not build reaches the site as its code and
// message.
func TestPublicHandlersAnswerTheFacadeError(t *testing.T) {
	for _, tc := range publicCases {
		t.Run(tc.call, func(t *testing.T) {
			port := &fakePublicInfo{Recorder: handlertest.Recorder{Err: xerr.Errorf(xerr.DatabaseQueryError, "settings unavailable")}}
			reply := handlertest.Serve(t, commonRoutes(port), http.MethodGet, tc.target, "")
			port.Called(t, tc.call, nil)
			reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
		})
	}
}
