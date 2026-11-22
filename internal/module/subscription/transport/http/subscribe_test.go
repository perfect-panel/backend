package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// subscribePath is the default subscription path the routes register the
// configuration endpoint on.
const subscribePath = "/v1/subscribe/config"

// deliveryCall is one delivery the handler asked the facade for.
type deliveryCall struct {
	ctx  context.Context
	meta subscription.RequestMeta
	req  dto.SubscribeRequest
}

// recordingDeliverer stands in for the subscription facade behind the
// Deliverer port: it answers the user-agent gate with allow (nil refuses
// every agent) and every delivery with resp or err, and records what the
// handler asked for.
type recordingDeliverer struct {
	allow      func(ctx context.Context, userAgent string) bool
	resp       *dto.SubscribeResponse
	err        error
	gated      []string
	deliveries []deliveryCall
}

var _ Deliverer = (*recordingDeliverer)(nil)

func (d *recordingDeliverer) IsUserAgentAllowed(ctx context.Context, userAgent string) bool {
	d.gated = append(d.gated, userAgent)
	return d.allow != nil && d.allow(ctx, userAgent)
}

func (d *recordingDeliverer) Deliver(ctx context.Context, meta subscription.RequestMeta, req *dto.SubscribeRequest) (*dto.SubscribeResponse, error) {
	d.deliveries = append(d.deliveries, deliveryCall{ctx: ctx, meta: meta, req: *req})
	if d.err != nil {
		return nil, d.err
	}
	return d.resp, nil
}

// onlyAgents allows exactly the listed user agents.
func onlyAgents(agents ...string) func(context.Context, string) bool {
	return func(_ context.Context, userAgent string) bool {
		for _, agent := range agents {
			if agent == userAgent {
				return true
			}
		}
		return false
	}
}

// yamlDelivery is a delivery as the facade renders one for a YAML client: the
// configuration, the traffic header and the download headers.
func yamlDelivery() *dto.SubscribeResponse {
	return &dto.SubscribeResponse{
		Config: []byte("proxies:\n  - name: node-1\n"),
		Header: "upload=10;download=20;total=1000;expire=1767225600",
		Headers: map[string]string{
			"Content-Disposition":     "attachment;filename*=UTF-8''Perfect%20Panel",
			"Content-Type":            "application/octet-stream; charset=UTF-8",
			"profile-update-interval": "24",
			"profile-web-page-url":    "https://panel.example.com/dashboard",
		},
	}
}

func subscribeSettings(cfg config.SubscribeConfig) func() config.SubscribeConfig {
	return func() config.SubscribeConfig { return cfg }
}

// serveSubscribe performs one GET against the delivery endpoints registered on
// their production routes: the subscription path, and the root the pan-domain
// mode adds.
func serveSubscribe(deps SubscribeDeps, url string, headers ...ut.Header) *ut.ResponseRecorder {
	engine := server.New()
	engine.GET(subscribePath, SubscribeHandler(deps))
	engine.GET("/", PanDomainSubscribeHandler(deps))
	return ut.PerformRequest(engine.Engine, http.MethodGet, url, nil, headers...)
}

func userAgent(value string) ut.Header { return ut.Header{Key: "User-Agent", Value: value} }

// assertAccessDenied checks the plain-text refusal of a request that never
// reached a delivery.
func assertAccessDenied(t *testing.T, w *ut.ResponseRecorder, facade *recordingDeliverer) {
	t.Helper()
	if w.Code != http.StatusForbidden || w.Body.String() != "Access denied" {
		t.Fatalf("response = %d %q, want 403 Access denied", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want plain text", got)
	}
	if got := w.Header().Get("subscription-userinfo"); got != "" {
		t.Fatalf("a refusal carries subscription-userinfo %q", got)
	}
	if len(facade.deliveries) != 0 {
		t.Fatalf("a refused request was delivered: %+v", facade.deliveries)
	}
}

// assertInternalServer checks the plain-text answer to a failed delivery: no
// configuration and none of the delivery headers.
func assertInternalServer(t *testing.T, w *ut.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusInternalServerError || w.Body.String() != "Internal Server" {
		t.Fatalf("response = %d %q, want 500 Internal Server", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want plain text", got)
	}
	if w.Header().Get("subscription-userinfo") != "" || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("a failed delivery carries delivery headers: %s", w.Header().Header())
	}
}

// assertDelivered checks that the client received yamlDelivery as the handler
// writes it.
func assertDelivered(t *testing.T, w *ut.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK || w.Body.String() != "proxies:\n  - name: node-1\n" {
		t.Fatalf("response = %d %q, want the rendered configuration", w.Code, w.Body.String())
	}
	for key, want := range map[string]string{
		// Clients show the used traffic and the expiry from this header.
		"subscription-userinfo":   "upload=10;download=20;total=1000;expire=1767225600",
		"Content-Disposition":     "attachment;filename*=UTF-8''Perfect%20Panel",
		"profile-update-interval": "24",
		"profile-web-page-url":    "https://panel.example.com/dashboard",
		// A downloadable format is served with the type the delivery asks
		// for, not the plain-text default.
		"Content-Type": "application/octet-stream; charset=UTF-8",
	} {
		if got := w.Header().Get(key); got != want {
			t.Errorf("header %s = %q, want %q", key, got, want)
		}
	}
}

func onlyDelivery(t *testing.T, facade *recordingDeliverer) deliveryCall {
	t.Helper()
	if len(facade.deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(facade.deliveries))
	}
	return facade.deliveries[0]
}

func TestSubscribeHandlerServesTheDeliveredConfiguration(t *testing.T) {
	facade := &recordingDeliverer{resp: yamlDelivery()}
	w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{})},
		"http://sub.example.com/v1/subscribe/config?token=tok-1&flag=clash&type=meta&mode=rule",
		userAgent("ClashMeta/1.18.0"), ut.Header{Key: "X-Real-IP", Value: "203.0.113.9"})

	assertDelivered(t, w)
	call := onlyDelivery(t, facade)
	// The facade selects the client application by the user agent, embeds
	// the request's URL in the configuration and records the client address.
	wantMeta := subscription.RequestMeta{
		Host:       "sub.example.com",
		RequestURI: "/v1/subscribe/config?token=tok-1&flag=clash&type=meta&mode=rule",
		UserAgent:  "ClashMeta/1.18.0",
		ClientIP:   "203.0.113.9",
	}
	if call.meta != wantMeta {
		t.Fatalf("meta = %+v, want %+v", call.meta, wantMeta)
	}
	wantReq := dto.SubscribeRequest{
		Token:  "tok-1",
		Params: map[string]string{"token": "tok-1", "flag": "clash", "type": "meta", "mode": "rule"},
	}
	if !reflect.DeepEqual(call.req, wantReq) {
		t.Fatalf("request = %+v, want %+v", call.req, wantReq)
	}
	if len(facade.gated) != 0 {
		t.Fatalf("the user-agent gate was consulted with the limit off: %v", facade.gated)
	}
}

// A base64 delivery asks for no download headers; the handler adds only the
// traffic header to it.
func TestSubscribeHandlerAddsOnlyTheTrafficHeaderToAPlainDelivery(t *testing.T) {
	facade := &recordingDeliverer{resp: &dto.SubscribeResponse{
		Config: []byte("c3M6Ly9ub2RlLTE="),
		Header: "upload=0;download=0;total=0;expire=0",
	}}
	w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{})},
		"/v1/subscribe/config?token=tok-1", userAgent("v2rayN/6.42"))

	if w.Code != http.StatusOK || w.Body.String() != "c3M6Ly9ub2RlLTE=" {
		t.Fatalf("response = %d %q, want the encoded configuration", w.Code, w.Body.String())
	}
	if got := w.Header().Get("subscription-userinfo"); got != "upload=0;download=0;total=0;expire=0" {
		t.Fatalf("subscription-userinfo = %q", got)
	}
	for _, key := range []string{"Content-Disposition", "profile-update-interval", "profile-web-page-url"} {
		if got := w.Header().Get(key); got != "" {
			t.Errorf("header %s = %q, want none", key, got)
		}
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want plain text", got)
	}
}

// Clients that cannot set headers put the token in the URL; the header wins
// when both are sent. Every query parameter reaches the template parameters,
// the first value of a repeated one included.
func TestSubscribeHandlerReadsTheTokenAndParametersFromTheRequest(t *testing.T) {
	for name, tt := range map[string]struct {
		url       string
		header    string
		wantToken string
		params    map[string]string
	}{
		"query token": {
			url: "/v1/subscribe/config?token=query-token", wantToken: "query-token",
			params: map[string]string{"token": "query-token"},
		},
		"header token wins": {
			url: "/v1/subscribe/config?token=query-token", header: "header-token", wantToken: "header-token",
			params: map[string]string{"token": "query-token"},
		},
		"no token": {
			url: "/v1/subscribe/config", params: map[string]string{},
		},
		"repeated parameters keep their first value": {
			url: "/v1/subscribe/config?token=t&flag=clash&flag=stash&mode=rule&mode=global&emoji", wantToken: "t",
			params: map[string]string{"token": "t", "flag": "clash", "mode": "rule", "emoji": ""},
		},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingDeliverer{resp: yamlDelivery()}
			headers := []ut.Header{userAgent("Shadowrocket/2070")}
			if tt.header != "" {
				headers = append(headers, ut.Header{Key: "token", Value: tt.header})
			}
			w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{})}, tt.url, headers...)

			assertDelivered(t, w)
			req := onlyDelivery(t, facade).req
			if req.Token != tt.wantToken || !reflect.DeepEqual(req.Params, tt.params) {
				t.Fatalf("request = %+v, want token %q params %v", req, tt.wantToken, tt.params)
			}
		})
	}
}

// Every refusal of the facade, whatever its cause, reaches the client as the
// same plain-text server error, without the traffic header.
func TestSubscribeHandlerAnswersDeliveryFailuresWithInternalServer(t *testing.T) {
	for name, err := range map[string]error{
		"unknown token":        xerr.Wrapf(gorm.ErrRecordNotFound, xerr.DatabaseQueryError, "find subscribe error"),
		"deleted account":      xerr.Errorf(xerr.UserNotExist, "User account does not exist"),
		"disabled account":     xerr.Errorf(xerr.UserDisabled, "User account is disabled"),
		"no client matches":    xerr.Errorf(xerr.ERROR, "No matching client found for user agent: curl/8.4"),
		"audit log failed":     xerr.Wrapf(errors.New("database is locked"), xerr.DatabaseInsertError, "insert subscription audit log"),
		"template build error": fmt.Errorf("build client config failed: %w", errors.New("template: bad")),
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingDeliverer{err: err}
			w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{})},
				"/v1/subscribe/config?token=tok-1", userAgent("ClashMeta/1.18.0"))

			assertInternalServer(t, w)
			if len(facade.deliveries) != 1 {
				t.Fatalf("deliveries = %d, want the one that failed", len(facade.deliveries))
			}
		})
	}
}

// A fetch over the per-address limit is answered with 429, so the client
// backs off instead of retrying a server error; it carries no delivery
// headers.
func TestSubscribeHandlerAnswersARateLimitedDeliveryWithTooManyRequests(t *testing.T) {
	facade := &recordingDeliverer{err: xerr.Errorf(xerr.TooManyRequests, "too many subscription fetches")}
	w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{})},
		"/v1/subscribe/config?token=tok-1", userAgent("ClashMeta/1.18.0"))

	if w.Code != http.StatusTooManyRequests || w.Body.String() != "Too Many Requests" {
		t.Fatalf("response = %d %q, want 429 Too Many Requests", w.Code, w.Body.String())
	}
	if w.Header().Get("subscription-userinfo") != "" || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("a refused fetch carries delivery headers: %s", w.Header().Header())
	}
}

// realDelivery is the subscription facade over the module's test harness with
// the delivery dependencies it reads before the owner's account: the client
// applications, plans, subscriptions and the audit log. The identity and
// network ports are absent, so only the paths that stop before them run.
func realDelivery(t *testing.T, cfg subscription.DeliveryConfig, apps ...client.SubscribeApplication) subscription.Service {
	t.Helper()
	f := subtest.New(t)
	if err := f.DB.AutoMigrate(&client.SubscribeApplication{}); err != nil {
		t.Fatalf("migrate client applications: %v", err)
	}
	for i := range apps {
		if err := f.DB.Create(&apps[i]).Error; err != nil {
			t.Fatalf("insert client application: %v", err)
		}
	}
	repos := subscription.NewRepoBuilder()(repository.ModuleConn{DB: f.DB, Redis: f.Redis}, nil)
	return subscription.New(subscription.Deps{
		Plans:          repos.Plans,
		UserSubs:       repos.UserSubs,
		Clients:        repos.Clients,
		Logs:           subtest.NewLogs(f.DB),
		DeliveryConfig: func() subscription.DeliveryConfig { return cfg },
	})
}

// With the limit on, the facade's allowlist decides: the configured keywords
// and the client applications' user agents, matched case-insensitively
// inside the User-Agent header.
func TestSubscribeHandlerGatesUserAgentsWithTheFacadeAllowlist(t *testing.T) {
	svc := realDelivery(t, subscription.DeliveryConfig{UserAgentList: "clash\nShadowrocket"},
		client.SubscribeApplication{Name: "Stash", UserAgent: "stash", SubscribeTemplate: "{{.SiteName}}", OutputFormat: "yaml"})
	for name, tt := range map[string]struct {
		userAgent string
		limit     bool
		served    bool
	}{
		"configured keyword":       {userAgent: "ClashMeta/1.18.0", limit: true, served: true},
		"keyword in another case":  {userAgent: "shadowrocket/2070 CFNetwork", limit: true, served: true},
		"client application agent": {userAgent: "Stash/2.4.6", limit: true, served: true},
		"unknown agent":            {userAgent: "curl/8.4.0", limit: true},
		"no user agent":            {limit: true},
		"unknown agent, limit off": {userAgent: "curl/8.4.0", served: true},
		"no user agent, limit off": {served: true},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingDeliverer{allow: svc.IsUserAgentAllowed, resp: yamlDelivery()}
			w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{UserAgentLimit: tt.limit})},
				"/v1/subscribe/config?token=tok-1", userAgent(tt.userAgent))

			if tt.served {
				assertDelivered(t, w)
			} else {
				assertAccessDenied(t, w, facade)
			}
			// Without the limit the gate is not consulted at all.
			wantGated := []string(nil)
			if tt.limit {
				wantGated = []string{tt.userAgent}
			}
			if !reflect.DeepEqual(facade.gated, wantGated) {
				t.Fatalf("gate calls = %q, want %q", facade.gated, wantGated)
			}
		})
	}
}

// The real facade refuses a token it cannot resolve, and a request no client
// application can serve, before it renders anything.
func TestSubscribeHandlerAnswersRealDeliveryRefusalsWithInternalServer(t *testing.T) {
	clash := client.SubscribeApplication{Name: "Clash", UserAgent: "clash", IsDefault: true, SubscribeTemplate: "{{.SiteName}}", OutputFormat: "yaml"}
	for name, apps := range map[string][]client.SubscribeApplication{
		"unknown token":         {clash},
		"no client application": nil,
	} {
		t.Run(name, func(t *testing.T) {
			deps := SubscribeDeps{Service: realDelivery(t, subscription.DeliveryConfig{SiteName: "Panel"}, apps...), Config: subscribeSettings(config.SubscribeConfig{})}
			w := serveSubscribe(deps, "/v1/subscribe/config?token=no-such-token", userAgent("ClashMeta/1.18.0"))

			assertInternalServer(t, w)
		})
	}
}

// In pan-domain mode every subscription has its own host, named by the
// token's eight-character short form; the configuration path serves a token
// only on that host. The short form mixes cases while the framework hands the
// handler a lowercased host, so only a case-insensitive comparison accepts it.
func TestSubscribeHandlerInPanDomainModeServesATokenOnlyOnItsHost(t *testing.T) {
	short, err := protocolkey.FixedUniqueString("tok-1", 8, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := protocolkey.FixedUniqueString("tok-2", 8, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct {
		host        string
		headerToken string
		served      bool
	}{
		"its short host":           {host: short + ".sub.example.com", served: true},
		"upper-cased host":         {host: strings.ToUpper(short) + ".sub.example.com", served: true},
		"lower-cased host":         {host: strings.ToLower(short) + ".sub.example.com", served: true},
		"host with a port":         {host: short + ".sub.example.com:8443", served: true},
		"single-label host":        {host: short, served: true},
		"another token's host":     {host: other + ".sub.example.com", served: false},
		"the long token as label":  {host: "tok-1.sub.example.com", served: false},
		"header token of the host": {host: other + ".sub.example.com", headerToken: "tok-2", served: true},
		"header token elsewhere":   {host: short + ".sub.example.com", headerToken: "tok-2", served: false},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingDeliverer{allow: onlyAgents("ClashMeta/1.18.0"), resp: yamlDelivery()}
			headers := []ut.Header{userAgent("ClashMeta/1.18.0")}
			if tt.headerToken != "" {
				headers = append(headers, ut.Header{Key: "token", Value: tt.headerToken})
			}
			deps := SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{PanDomain: true, UserAgentLimit: true})}
			w := serveSubscribe(deps, "http://"+tt.host+subscribePath+"?token=tok-1", headers...)

			if !tt.served {
				assertAccessDenied(t, w, facade)
				// The host is checked before the user agent.
				if len(facade.gated) != 0 {
					t.Fatalf("the user-agent gate was consulted for a foreign host: %v", facade.gated)
				}
				return
			}
			assertDelivered(t, w)
			if call := onlyDelivery(t, facade); call.meta.Host != strings.ToLower(tt.host) {
				t.Fatalf("delivered host = %q, want %q lowercased", call.meta.Host, tt.host)
			}
		})
	}
}

// The pan-domain root takes the whole token from the first host label; a
// token in the header or the URL does not
// select the subscription there. The host arrives lowercased, and so does the
// token read from it: subscription tokens are lowercase hex.
func TestPanDomainSubscribeHandlerSelectsTheSubscriptionByHost(t *testing.T) {
	facade := &recordingDeliverer{resp: yamlDelivery()}
	w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{PanDomain: true})},
		"http://TOK-1.Clash.sub.example.com/?mode=rule&token=tok-2",
		userAgent("ClashMeta/1.18.0"), ut.Header{Key: "token", Value: "tok-3"}, ut.Header{Key: "X-Forwarded-For", Value: "198.51.100.4"})

	assertDelivered(t, w)
	call := onlyDelivery(t, facade)
	wantMeta := subscription.RequestMeta{
		Host: "tok-1.clash.sub.example.com", RequestURI: "/?mode=rule&token=tok-2",
		UserAgent: "ClashMeta/1.18.0", ClientIP: "198.51.100.4",
	}
	if call.meta != wantMeta {
		t.Fatalf("meta = %+v, want %+v", call.meta, wantMeta)
	}
	wantReq := dto.SubscribeRequest{
		Token:  "tok-1",
		Params: map[string]string{"mode": "rule", "token": "tok-2"},
	}
	if !reflect.DeepEqual(call.req, wantReq) {
		t.Fatalf("request = %+v, want %+v", call.req, wantReq)
	}
}

func TestPanDomainSubscribeHandlerRefusesForeignHostsAndAgents(t *testing.T) {
	for name, tt := range map[string]struct {
		host      string
		limit     bool
		userAgent string
		gated     []string
	}{
		// Without a second label the host is not a subscription host.
		"single-label host": {host: "localhost", userAgent: "ClashMeta/1.18.0"},
		// The user agent is checked before the host.
		"refused user agent":                   {host: "tok-1.clash.sub.example.com", limit: true, userAgent: "curl/8.4.0", gated: []string{"curl/8.4.0"}},
		"refused user agent on a single label": {host: "localhost", limit: true, userAgent: "curl/8.4.0", gated: []string{"curl/8.4.0"}},
		"allowed user agent on a single label": {host: "localhost", limit: true, userAgent: "ClashMeta/1.18.0", gated: []string{"ClashMeta/1.18.0"}},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingDeliverer{allow: onlyAgents("ClashMeta/1.18.0"), resp: yamlDelivery()}
			w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{PanDomain: true, UserAgentLimit: tt.limit})},
				"http://"+tt.host+"/", userAgent(tt.userAgent))

			assertAccessDenied(t, w, facade)
			if !reflect.DeepEqual(facade.gated, tt.gated) {
				t.Fatalf("gate calls = %q, want %q", facade.gated, tt.gated)
			}
		})
	}
}

func TestPanDomainSubscribeHandlerAnswersDeliveryFailuresWithInternalServer(t *testing.T) {
	facade := &recordingDeliverer{allow: onlyAgents("ClashMeta/1.18.0"), err: xerr.Errorf(xerr.UserDisabled, "User account is disabled")}
	w := serveSubscribe(SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{PanDomain: true, UserAgentLimit: true})},
		"http://tok-1.clash.sub.example.com/", userAgent("ClashMeta/1.18.0"))

	assertInternalServer(t, w)
	if call := onlyDelivery(t, facade); call.req.Token != "tok-1" {
		t.Fatalf("delivered token = %q, want tok-1", call.req.Token)
	}
}

// The facade's audit record reads the request metadata (IP location, actor)
// the middleware put on the request context, so both endpoints must hand
// that context on.
func TestSubscribeHandlersHandTheRequestContextToTheFacade(t *testing.T) {
	facade := &recordingDeliverer{resp: yamlDelivery()}
	deps := SubscribeDeps{Service: facade, Config: subscribeSettings(config.SubscribeConfig{})}
	engine := server.New()
	engine.Use(func(c context.Context, ctx *app.RequestContext) {
		ctx.Next(requestmeta.With(c, requestmeta.Metadata{ClientIP: "192.0.2.10", IPMetadata: requestmeta.IPMetadata{IPCountryCode: "NL"}}))
	})
	engine.GET(subscribePath, SubscribeHandler(deps))
	engine.GET("/", PanDomainSubscribeHandler(deps))

	for _, url := range []string{"/v1/subscribe/config?token=tok-1", "http://tok-1.clash.sub.example.com/"} {
		ut.PerformRequest(engine.Engine, http.MethodGet, url, nil, userAgent("ClashMeta/1.18.0"))
	}
	if len(facade.deliveries) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(facade.deliveries))
	}
	for _, call := range facade.deliveries {
		if metadata, ok := requestmeta.From(call.ctx); !ok || metadata.IPCountryCode != "NL" || metadata.ClientIP != "192.0.2.10" {
			t.Fatalf("delivery context metadata = %+v (%v), want the middleware's", metadata, ok)
		}
	}
}
