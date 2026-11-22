package system

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/transport/http/internal/handlertest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// fakeSettings is the settings port: it records every call and answers the
// recorder's answer for the method.
type fakeSettings struct{ handlertest.Recorder }

var _ Settings = (*fakeSettings)(nil)

func (f *fakeSettings) GetCurrencyConfig(context.Context) (*dto.CurrencyConfig, error) {
	return handlertest.Answer[dto.CurrencyConfig](&f.Recorder, "GetCurrencyConfig", nil)
}

func (f *fakeSettings) GetInviteConfig(context.Context) (*dto.InviteConfig, error) {
	return handlertest.Answer[dto.InviteConfig](&f.Recorder, "GetInviteConfig", nil)
}

func (f *fakeSettings) GetNodeConfig(context.Context) (*dto.NodeConfig, error) {
	return handlertest.Answer[dto.NodeConfig](&f.Recorder, "GetNodeConfig", nil)
}

func (f *fakeSettings) GetNodeMultiplier(context.Context) (*dto.GetNodeMultiplierResponse, error) {
	return handlertest.Answer[dto.GetNodeMultiplierResponse](&f.Recorder, "GetNodeMultiplier", nil)
}

func (f *fakeSettings) GetPrivacyPolicyConfig(context.Context) (*dto.PrivacyPolicyConfig, error) {
	return handlertest.Answer[dto.PrivacyPolicyConfig](&f.Recorder, "GetPrivacyPolicyConfig", nil)
}

func (f *fakeSettings) GetRegisterConfig(context.Context) (*dto.RegisterConfig, error) {
	return handlertest.Answer[dto.RegisterConfig](&f.Recorder, "GetRegisterConfig", nil)
}

func (f *fakeSettings) GetSiteConfig(context.Context) (*dto.SiteConfig, error) {
	return handlertest.Answer[dto.SiteConfig](&f.Recorder, "GetSiteConfig", nil)
}

func (f *fakeSettings) GetSubscribeConfig(context.Context) (*dto.SubscribeConfig, error) {
	return handlertest.Answer[dto.SubscribeConfig](&f.Recorder, "GetSubscribeConfig", nil)
}

func (f *fakeSettings) GetTosConfig(context.Context) (*dto.TosConfig, error) {
	return handlertest.Answer[dto.TosConfig](&f.Recorder, "GetTosConfig", nil)
}

func (f *fakeSettings) GetVerifyCodeConfig(context.Context) (*dto.VerifyCodeConfig, error) {
	return handlertest.Answer[dto.VerifyCodeConfig](&f.Recorder, "GetVerifyCodeConfig", nil)
}

func (f *fakeSettings) GetVerifyConfig(context.Context) (*dto.VerifyConfig, error) {
	return handlertest.Answer[dto.VerifyConfig](&f.Recorder, "GetVerifyConfig", nil)
}

func (f *fakeSettings) PreViewNodeMultiplier(context.Context) (*dto.PreViewNodeMultiplierResponse, error) {
	return handlertest.Answer[dto.PreViewNodeMultiplierResponse](&f.Recorder, "PreViewNodeMultiplier", nil)
}

func (f *fakeSettings) SetNodeMultiplier(_ context.Context, req *dto.SetNodeMultiplierRequest) error {
	return f.Do("SetNodeMultiplier", req)
}

func (f *fakeSettings) SettingTelegramBot(context.Context) error {
	return f.Do("SettingTelegramBot", nil)
}

func (f *fakeSettings) UpdateCurrencyConfig(_ context.Context, req *dto.CurrencyConfig) error {
	return f.Do("UpdateCurrencyConfig", req)
}

func (f *fakeSettings) UpdateInviteConfig(_ context.Context, req *dto.InviteConfig) error {
	return f.Do("UpdateInviteConfig", req)
}

func (f *fakeSettings) UpdateNodeConfig(_ context.Context, req *dto.NodeConfig) error {
	return f.Do("UpdateNodeConfig", req)
}

func (f *fakeSettings) UpdatePrivacyPolicyConfig(_ context.Context, req *dto.PrivacyPolicyConfig) error {
	return f.Do("UpdatePrivacyPolicyConfig", req)
}

func (f *fakeSettings) UpdateRegisterConfig(_ context.Context, req *dto.RegisterConfig) error {
	return f.Do("UpdateRegisterConfig", req)
}

func (f *fakeSettings) UpdateSiteConfig(_ context.Context, req *dto.SiteConfig) error {
	return f.Do("UpdateSiteConfig", req)
}

func (f *fakeSettings) UpdateSubscribeConfig(_ context.Context, req *dto.SubscribeConfig) error {
	return f.Do("UpdateSubscribeConfig", req)
}

func (f *fakeSettings) UpdateTosConfig(_ context.Context, req *dto.TosConfig) error {
	return f.Do("UpdateTosConfig", req)
}

func (f *fakeSettings) UpdateVerifyCodeConfig(_ context.Context, req *dto.VerifyCodeConfig) error {
	return f.Do("UpdateVerifyCodeConfig", req)
}

func (f *fakeSettings) UpdateVerifyConfig(_ context.Context, req *dto.VerifyConfig) error {
	return f.Do("UpdateVerifyConfig", req)
}

// systemRoutes registers the setting handlers on their production routes.
func systemRoutes(port Settings) *server.Hertz {
	h := server.New()
	group := h.Group("/v1/admin/system")
	group.GET("/currency_config", GetCurrencyConfigHandler(port))
	group.PUT("/currency_config", UpdateCurrencyConfigHandler(port))
	group.GET("/get_node_multiplier", GetNodeMultiplierHandler(port))
	group.GET("/invite_config", GetInviteConfigHandler(port))
	group.PUT("/invite_config", UpdateInviteConfigHandler(port))
	group.GET("/node_config", GetNodeConfigHandler(port))
	group.PUT("/node_config", UpdateNodeConfigHandler(port))
	group.GET("/node_multiplier/preview", PreViewNodeMultiplierHandler(port))
	group.GET("/privacy", GetPrivacyPolicyConfigHandler(port))
	group.PUT("/privacy", UpdatePrivacyPolicyConfigHandler(port))
	group.GET("/register_config", GetRegisterConfigHandler(port))
	group.PUT("/register_config", UpdateRegisterConfigHandler(port))
	group.POST("/set_node_multiplier", SetNodeMultiplierHandler(port))
	group.POST("/setting_telegram_bot", SettingTelegramBotHandler(port))
	group.GET("/site_config", GetSiteConfigHandler(port))
	group.PUT("/site_config", UpdateSiteConfigHandler(port))
	group.GET("/subscribe_config", GetSubscribeConfigHandler(port))
	group.PUT("/subscribe_config", UpdateSubscribeConfigHandler(port))
	group.GET("/tos_config", GetTosConfigHandler(port))
	group.PUT("/tos_config", UpdateTosConfigHandler(port))
	group.GET("/verify_code_config", GetVerifyCodeConfigHandler(port))
	group.PUT("/verify_code_config", UpdateVerifyCodeConfigHandler(port))
	group.GET("/verify_config", GetVerifyConfigHandler(port))
	group.PUT("/verify_config", UpdateVerifyConfigHandler(port))
	return h
}

type settingCase struct {
	name, httpMethod, path, body string
	call                         string
	request                      any // the request the port receives
	answer                       any // the port's answer, the reply's data
}

var settingCases = []settingCase{
	{name: "currency", httpMethod: http.MethodGet, path: "/currency_config", call: "GetCurrencyConfig",
		answer: &dto.CurrencyConfig{AccessKey: "key", CurrencyUnit: "USD", CurrencySymbol: "$"}},
	{name: "update currency", httpMethod: http.MethodPut, path: "/currency_config", call: "UpdateCurrencyConfig",
		body:    `{"access_key":"key","currency_unit":"EUR","currency_symbol":"€"}`,
		request: &dto.CurrencyConfig{AccessKey: "key", CurrencyUnit: "EUR", CurrencySymbol: "€"}},
	{name: "node multiplier", httpMethod: http.MethodGet, path: "/get_node_multiplier", call: "GetNodeMultiplier",
		answer: &dto.GetNodeMultiplierResponse{Periods: []dto.TimePeriod{{StartTime: "00:00", EndTime: "06:00", Multiplier: 0.5}}}},
	{name: "invite", httpMethod: http.MethodGet, path: "/invite_config", call: "GetInviteConfig",
		answer: &dto.InviteConfig{ForcedInvite: true, ReferralPercentage: 20, OnlyFirstPurchase: true, WithdrawalMethod: "usdt"}},
	{name: "update invite", httpMethod: http.MethodPut, path: "/invite_config", call: "UpdateInviteConfig",
		body:    `{"forced_invite":false,"referral_percentage":15,"only_first_purchase":true,"withdrawal_method":"alipay"}`,
		request: &dto.InviteConfig{ReferralPercentage: 15, OnlyFirstPurchase: true, WithdrawalMethod: "alipay"}},
	{name: "node", httpMethod: http.MethodGet, path: "/node_config", call: "GetNodeConfig",
		answer: &dto.NodeConfig{NodeSecret: "secret", NodePullInterval: 60, NodePushInterval: 30, IPStrategy: "prefer_ipv4",
			DNS: []dto.PlatformNodeDNSSnapshot{{Proto: "udp", Address: "1.1.1.1:53", Domains: []string{"example.com"}}}, Block: []string{"ads.example"}}},
	{name: "update node", httpMethod: http.MethodPut, path: "/node_config", call: "UpdateNodeConfig",
		body: `{"node_secret":"secret","node_pull_interval":60,"node_push_interval":30,"traffic_report_threshold":1024,"ip_strategy":"prefer_ipv6",` +
			`"dns":[{"proto":"tcp","address":"8.8.8.8:53","domains":["example.org"]}],"block":["tracker.example"],` +
			`"outbound":[{"name":"relay","protocol":"shadowsocks","address":"relay.example","port":8388,"password":"pw","cipher":"aes-256-gcm","plugin_opts":{"mode":"fast"},"rules":["geosite:netflix"]}]}`,
		request: &dto.NodeConfig{NodeSecret: "secret", NodePullInterval: 60, NodePushInterval: 30, TrafficReportThreshold: 1024, IPStrategy: "prefer_ipv6",
			DNS:   []dto.PlatformNodeDNSSnapshot{{Proto: "tcp", Address: "8.8.8.8:53", Domains: []string{"example.org"}}},
			Block: []string{"tracker.example"},
			Outbound: []dto.PlatformNodeOutboundSnapshot{{Name: "relay", Protocol: "shadowsocks", Address: "relay.example", Port: 8388, Password: "pw",
				Cipher: "aes-256-gcm", PluginOptions: map[string]any{"mode": "fast"}, Rules: []string{"geosite:netflix"}}}}},
	{name: "multiplier preview", httpMethod: http.MethodGet, path: "/node_multiplier/preview", call: "PreViewNodeMultiplier",
		answer: &dto.PreViewNodeMultiplierResponse{CurrentTime: "03:00", Ratio: 0.5}},
	{name: "privacy", httpMethod: http.MethodGet, path: "/privacy", call: "GetPrivacyPolicyConfig",
		answer: &dto.PrivacyPolicyConfig{PrivacyPolicy: "# Privacy"}},
	{name: "update privacy", httpMethod: http.MethodPut, path: "/privacy", call: "UpdatePrivacyPolicyConfig",
		body: `{"privacy_policy":"# Privacy\n\nWe keep nothing."}`, request: &dto.PrivacyPolicyConfig{PrivacyPolicy: "# Privacy\n\nWe keep nothing."}},
	{name: "register", httpMethod: http.MethodGet, path: "/register_config", call: "GetRegisterConfig",
		answer: &dto.RegisterConfig{EnableTrial: true, TrialSubscribe: 2, TrialTime: 3, TrialTimeUnit: "Day"}},
	{name: "update register", httpMethod: http.MethodPut, path: "/register_config", call: "UpdateRegisterConfig",
		body:    `{"stop_register":true,"enable_trial":false,"trial_subscribe":0,"trial_time":0,"trial_time_unit":"","enable_ip_register_limit":true,"ip_register_limit":3,"ip_register_limit_duration":3600}`,
		request: &dto.RegisterConfig{StopRegister: true, EnableIpRegisterLimit: true, IpRegisterLimit: 3, IpRegisterLimitDuration: 3600}},
	{name: "set node multiplier", httpMethod: http.MethodPost, path: "/set_node_multiplier", call: "SetNodeMultiplier",
		body:    `{"periods":[{"start_time":"00:00","end_time":"06:00","multiplier":0.5},{"start_time":"20:00","end_time":"23:59","multiplier":2}]}`,
		request: &dto.SetNodeMultiplierRequest{Periods: []dto.TimePeriod{{StartTime: "00:00", EndTime: "06:00", Multiplier: 0.5}, {StartTime: "20:00", EndTime: "23:59", Multiplier: 2}}}},
	{name: "telegram bot", httpMethod: http.MethodPost, path: "/setting_telegram_bot", call: "SettingTelegramBot"},
	{name: "site", httpMethod: http.MethodGet, path: "/site_config", call: "GetSiteConfig",
		answer: &dto.SiteConfig{Host: "panel.example", SiteName: "Panel", SiteLogo: "/logo.svg"}},
	{name: "update site", httpMethod: http.MethodPut, path: "/site_config", call: "UpdateSiteConfig",
		body:    `{"host":"panel.example","site_name":"Panel","site_desc":"fast","site_logo":"/logo.svg","keywords":"vpn","custom_html":"<b>hi</b>","custom_data":"{}"}`,
		request: &dto.SiteConfig{Host: "panel.example", SiteName: "Panel", SiteDesc: "fast", SiteLogo: "/logo.svg", Keywords: "vpn", CustomHTML: "<b>hi</b>", CustomData: "{}"}},
	{name: "subscribe", httpMethod: http.MethodGet, path: "/subscribe_config", call: "GetSubscribeConfig",
		answer: &dto.SubscribeConfig{SubscribePath: "/sub", SubscribeDomain: "sub.example", UserAgentLimit: true, UserAgentList: "clash"}},
	{name: "update subscribe", httpMethod: http.MethodPut, path: "/subscribe_config", call: "UpdateSubscribeConfig",
		body:    `{"single_model":true,"subscribe_path":"/s","subscribe_domain":"s.example","pan_domain":true,"show_tutorial":true,"profile_update_interval":12,"profile_web_page_url":"https://panel.example"}`,
		request: &dto.SubscribeConfig{SingleModel: true, SubscribePath: "/s", SubscribeDomain: "s.example", PanDomain: true, ShowTutorial: true, ProfileUpdateInterval: 12, ProfileWebPageURL: "https://panel.example"}},
	{name: "tos", httpMethod: http.MethodGet, path: "/tos_config", call: "GetTosConfig",
		answer: &dto.TosConfig{TosContent: "# Terms"}},
	{name: "update tos", httpMethod: http.MethodPut, path: "/tos_config", call: "UpdateTosConfig",
		body: `{"tos_content":"# Terms v2"}`, request: &dto.TosConfig{TosContent: "# Terms v2"}},
	{name: "verify code", httpMethod: http.MethodGet, path: "/verify_code_config", call: "GetVerifyCodeConfig",
		answer: &dto.VerifyCodeConfig{VerifyCodeExpireTime: 300, VerifyCodeLimit: 15, VerifyCodeInterval: 60}},
	{name: "update verify code", httpMethod: http.MethodPut, path: "/verify_code_config", call: "UpdateVerifyCodeConfig",
		body: `{"verify_code_expire_time":600,"verify_code_limit":10,"verify_code_interval":90}`, request: &dto.VerifyCodeConfig{VerifyCodeExpireTime: 600, VerifyCodeLimit: 10, VerifyCodeInterval: 90}},
	{name: "verify", httpMethod: http.MethodGet, path: "/verify_config", call: "GetVerifyConfig",
		answer: &dto.VerifyConfig{TurnstileSiteKey: "site-key", TurnstileSecret: "secret", EnableLoginVerify: true}},
	{name: "update verify", httpMethod: http.MethodPut, path: "/verify_config", call: "UpdateVerifyConfig",
		body:    `{"turnstile_site_key":"site-key","turnstile_secret":"secret","enable_login_verify":false,"enable_register_verify":true,"enable_reset_password_verify":true}`,
		request: &dto.VerifyConfig{TurnstileSiteKey: "site-key", TurnstileSecret: "secret", EnableRegisterVerify: true, EnableResetPasswordVerify: true}},
}

// Each setting route reads its setting, or binds the setting the admin panel
// sent and hands it to the facade, and answers the success envelope.
func TestSettingHandlersCallTheFacadeWithTheBoundSetting(t *testing.T) {
	for _, tc := range settingCases {
		t.Run(tc.name, func(t *testing.T) {
			port := &fakeSettings{Recorder: handlertest.Recorder{Answers: map[string]any{tc.call: tc.answer}}}
			reply := handlertest.Serve(t, systemRoutes(port), tc.httpMethod, "/v1/admin/system"+tc.path, tc.body)
			port.Called(t, tc.call, tc.request)
			reply.OK(t, tc.answer)
		})
	}
}

// A setting the facade could not read or apply reaches the admin panel as
// the failure's code and message.
func TestSettingHandlersAnswerTheFacadeError(t *testing.T) {
	for _, tc := range settingCases {
		t.Run(tc.name, func(t *testing.T) {
			port := &fakeSettings{Recorder: handlertest.Recorder{Err: xerr.Errorf(xerr.DatabaseUpdateError, "settings unavailable")}}
			reply := handlertest.Serve(t, systemRoutes(port), tc.httpMethod, "/v1/admin/system"+tc.path, tc.body)
			port.Called(t, tc.call, tc.request)
			reply.Refused(t, xerr.DatabaseUpdateError, "Database update error")
		})
	}
}

// A setting body that is not the setting's JSON document is refused as a
// parameter error, and nothing is applied.
func TestSettingHandlersRefuseMalformedBodies(t *testing.T) {
	for _, tc := range settingCases {
		if tc.body == "" {
			continue
		}
		for name, body := range map[string]string{"truncated": tc.body[:len(tc.body)/2], "not an object": `["` + tc.name + `"]`} {
			t.Run(tc.name+" "+name, func(t *testing.T) {
				port := &fakeSettings{}
				reply := handlertest.Serve(t, systemRoutes(port), tc.httpMethod, "/v1/admin/system"+tc.path, body)
				reply.Refused(t, xerr.InvalidParams, "")
				if len(port.Calls) != 0 {
					t.Fatalf("calls = %+v, want nothing applied", port.Calls)
				}
			})
		}
	}
}
