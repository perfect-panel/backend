package edge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"
	"uuid"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/redis/go-redis/v9"
)

const (
	edgeKeyID    = "edge-a"
	edgeSecret   = "edge-test-secret"
	manifestPath = "/api/edge/v1/manifest"
)

// manifests stands in for the network facade's manifest builder: it answers
// with the canned result and records the tokens it was asked for.
type manifests struct {
	manifest *dto.EdgeManifestResponse
	err      error
	tokens   []string
}

var _ ManifestBuilder = (*manifests)(nil)

func (m *manifests) EdgeManifest(_ context.Context, token string) (*dto.EdgeManifestResponse, error) {
	m.tokens = append(m.tokens, token)
	return m.manifest, m.err
}

func edgeConfig() config.EdgeSubscribeConfig {
	return config.EdgeSubscribeConfig{
		Enabled: true,
		Keys:    []config.EdgeSubscribeAccessKey{{ID: edgeKeyID, Secret: edgeSecret}},
	}
}

func sampleManifest() *dto.EdgeManifestResponse {
	return &dto.EdgeManifestResponse{
		SchemaVersion: "1",
		Revision:      "rev-1",
		GeneratedAt:   "2026-09-28T00:00:00Z",
		Subscription:  dto.EdgeManifestSubscription{Name: "Pro", State: "active", TrafficLimit: 1 << 30, Upload: 10, Download: 20},
		Proxies:       []dto.EdgeManifestProxy{{Name: "tokyo", Protocol: "vless", Server: "tokyo.example", Port: 443, UUID: "uuid-1", UDP: true, Sort: 1}},
		Notices:       []string{},
	}
}

// replayCache returns a Redis client on a fresh miniredis and the server.
func replayCache(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, mini
}

// manifestEndpoint serves the manifest route with the given dependencies.
func manifestEndpoint(deps ManifestDeps) *server.Hertz {
	h := server.Default()
	h.GET(manifestPath, ManifestHandler(deps))
	return h
}

// edgeRequest is a manifest request as the edge Worker signs it.
type edgeRequest struct {
	token     string
	kid       string
	secret    string
	requestID string
	signedAt  time.Time
	// signedToken is the token the signature covers, when it is not token.
	signedToken *string
}

func signedRequest(token string) edgeRequest {
	return edgeRequest{token: token, kid: edgeKeyID, secret: edgeSecret, requestID: uuid.New().String(), signedAt: time.Now()}
}

// authorization is the PPanel-Edge-HMAC credential over
// v2\nGET\n/api/edge/v1/manifest\nSHA256(token)\nts\nSHA256(requestID).
func (r edgeRequest) authorization() string {
	token := r.token
	if r.signedToken != nil {
		token = *r.signedToken
	}
	timestamp := strconv.FormatInt(r.signedAt.Unix(), 10)
	tokenHash := sha256.Sum256([]byte(token))
	requestIDHash := sha256.Sum256([]byte(r.requestID))
	mac := hmac.New(sha256.New, []byte(r.secret))
	_, _ = fmt.Fprintf(mac, "v2\nGET\n%s\n%s\n%s\n%s", manifestPath, hex.EncodeToString(tokenHash[:]), timestamp, hex.EncodeToString(requestIDHash[:]))
	return "PPanel-Edge-HMAC kid=" + r.kid + ", ts=" + timestamp + ", sig=" + hex.EncodeToString(mac.Sum(nil))
}

func (r edgeRequest) send(h *server.Hertz) *protocol.Response {
	return ut.PerformRequest(h.Engine, http.MethodGet, manifestPath+"?token="+url.QueryEscape(r.token), nil,
		ut.Header{Key: "Authorization", Value: r.authorization()},
		ut.Header{Key: "X-Request-ID", Value: r.requestID},
	).Result()
}

func assertNotFound(t *testing.T, resp *protocol.Response) {
	t.Helper()
	assertText(t, resp, http.StatusNotFound, "Not Found")
}

func assertText(t *testing.T, resp *protocol.Response, status int, want string) {
	t.Helper()
	if resp.StatusCode() != status || string(resp.Body()) != want {
		t.Fatalf("response = %d %q, want %d %q", resp.StatusCode(), resp.Body(), status, want)
	}
	// A refusal carries no Cache-Control; only a served manifest is marked
	// no-store.
	if got := string(resp.Header.Peek("Cache-Control")); got != "" {
		t.Fatalf("Cache-Control = %q on a refusal, want none", got)
	}
}

// A signed request gets the facade's manifest for its token as plain JSON,
// without the application's result envelope, and marked no-store: it holds
// the user's credentials.
func TestManifestHandler_servesTheManifestToASignedRequest(t *testing.T) {
	facade := &manifests{manifest: sampleManifest()}
	client, mini := replayCache(t)
	h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: edgeConfig})

	resp := signedRequest("sub-token").send(h)

	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	if got := string(resp.Header.ContentType()); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", got)
	}
	if got := string(resp.Header.Peek("Cache-Control")); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var got dto.EdgeManifestResponse
	if err := json.Unmarshal(resp.Body(), &got); err != nil {
		t.Fatalf("decode manifest %q: %v", resp.Body(), err)
	}
	if !reflect.DeepEqual(&got, sampleManifest()) {
		t.Fatalf("manifest = %+v, want the facade's", got)
	}
	if !reflect.DeepEqual(facade.tokens, []string{"sub-token"}) {
		t.Fatalf("facade tokens = %q, want the request's", facade.tokens)
	}
	if keys := mini.Keys(); len(keys) != 1 {
		t.Fatalf("replay cache keys = %q, want the request ID claimed", keys)
	}
}

// The token is trimmed before the signature is checked and the manifest
// looked up, so the signature covers the trimmed token.
func TestManifestHandler_trimsTheToken(t *testing.T) {
	facade := &manifests{manifest: sampleManifest()}
	client, _ := replayCache(t)
	h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: edgeConfig})
	request := signedRequest(" sub-token\t")
	trimmed := "sub-token"
	request.signedToken = &trimmed

	if resp := request.send(h); resp.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	if !reflect.DeepEqual(facade.tokens, []string{"sub-token"}) {
		t.Fatalf("facade tokens = %q, want the trimmed token", facade.tokens)
	}
}

// Every rejected credential is the same 404 an unknown token gets, so a
// caller learns nothing about which part failed. A rejected request neither
// reaches the facade nor spends its request ID.
func TestManifestHandler_hidesRejectedCredentialsBehindNotFound(t *testing.T) {
	otherToken := "other-token"
	for name, tc := range map[string]struct {
		// forge changes the signed request for "sub-token".
		forge func(*edgeRequest)
		// config changes the runtime configuration.
		config func(*config.EdgeSubscribeConfig)
	}{
		"wrong secret":        {forge: func(r *edgeRequest) { r.secret = "guess" }},
		"unknown key":         {forge: func(r *edgeRequest) { r.kid = "edge-z" }},
		"stale signature":     {forge: func(r *edgeRequest) { r.signedAt = time.Now().Add(-10 * time.Minute) }},
		"future signature":    {forge: func(r *edgeRequest) { r.signedAt = time.Now().Add(10 * time.Minute) }},
		"request ID not UUID": {forge: func(r *edgeRequest) { r.requestID = "request-1" }},
		"another token":       {forge: func(r *edgeRequest) { r.signedToken = &otherToken }},
		"no token":            {forge: func(r *edgeRequest) { r.token = "" }},
		"edge disabled":       {config: func(cfg *config.EdgeSubscribeConfig) { cfg.Enabled = false }},
		"key without secret":  {config: func(cfg *config.EdgeSubscribeConfig) { cfg.Keys[0].Secret = "" }},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &manifests{manifest: sampleManifest()}
			client, mini := replayCache(t)
			cfg := edgeConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: func() config.EdgeSubscribeConfig { return cfg }})
			request := signedRequest("sub-token")
			if tc.forge != nil {
				tc.forge(&request)
			}

			assertNotFound(t, request.send(h))
			if len(facade.tokens) != 0 || len(mini.Keys()) != 0 {
				t.Fatalf("facade tokens %q, replay keys %q: want neither", facade.tokens, mini.Keys())
			}
		})
	}
	t.Run("no credential", func(t *testing.T) {
		facade := &manifests{manifest: sampleManifest()}
		client, _ := replayCache(t)
		h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: edgeConfig})

		assertNotFound(t, ut.PerformRequest(h.Engine, http.MethodGet, manifestPath+"?token=sub-token", nil).Result())
		if len(facade.tokens) != 0 {
			t.Fatalf("facade tokens = %q, want none", facade.tokens)
		}
	})
}

// A signed request is good once: its request ID is claimed, and a replay of
// it gets the same 404 as a forgery.
func TestManifestHandler_rejectsAReplayedRequest(t *testing.T) {
	facade := &manifests{manifest: sampleManifest()}
	client, _ := replayCache(t)
	h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: edgeConfig})
	request := signedRequest("sub-token")

	if resp := request.send(h); resp.StatusCode() != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", resp.StatusCode())
	}
	assertNotFound(t, request.send(h))
	if len(facade.tokens) != 1 {
		t.Fatalf("facade was asked %d times, want once", len(facade.tokens))
	}
}

// Without the replay cache a signature could be replayed, so the endpoint
// fails closed with a 503 and does not build the manifest.
func TestManifestHandler_failsClosedWithoutTheReplayCache(t *testing.T) {
	logtest.Discard(t)
	unavailable, mini := replayCache(t)
	mini.SetError("ERR replay cache unavailable")
	for name, client := range map[string]*redis.Client{
		"cache error": unavailable,
		"no cache":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			facade := &manifests{manifest: sampleManifest()}
			h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: edgeConfig})

			assertText(t, signedRequest("sub-token").send(h), http.StatusServiceUnavailable, "Service Unavailable")
			if len(facade.tokens) != 0 {
				t.Fatalf("facade tokens = %q, want none", facade.tokens)
			}
		})
	}
}

// An unknown token gets the same 404 as a rejected credential; any other
// failure of the facade is a 500 that does not describe it.
func TestManifestHandler_mapsFacadeErrors(t *testing.T) {
	logtest.Discard(t)
	for name, tc := range map[string]struct {
		err        error
		wantStatus int
		wantBody   string
	}{
		"unknown token":         {network.ErrManifestNotFound, http.StatusNotFound, "Not Found"},
		"wrapped unknown token": {fmt.Errorf("find subscription: %w", network.ErrManifestNotFound), http.StatusNotFound, "Not Found"},
		"other failure":         {errors.New("database is down"), http.StatusInternalServerError, "Internal Server Error"},
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := replayCache(t)
			h := manifestEndpoint(ManifestDeps{Network: &manifests{err: tc.err}, Redis: client, Config: edgeConfig})

			assertText(t, signedRequest("sub-token").send(h), tc.wantStatus, tc.wantBody)
		})
	}
}

// The runtime configuration is read per request, so a rotated key applies
// without restarting the handler.
func TestManifestHandler_readsTheConfigurationPerRequest(t *testing.T) {
	facade := &manifests{manifest: sampleManifest()}
	client, _ := replayCache(t)
	current := edgeConfig()
	h := manifestEndpoint(ManifestDeps{Network: facade, Redis: client, Config: func() config.EdgeSubscribeConfig { return current }})

	if resp := signedRequest("sub-token").send(h); resp.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200 before the rotation", resp.StatusCode())
	}
	current = config.EdgeSubscribeConfig{Enabled: true, Keys: []config.EdgeSubscribeAccessKey{{ID: "edge-b", Secret: "rotated"}}}
	assertNotFound(t, signedRequest("sub-token").send(h))
	rotated := signedRequest("sub-token")
	rotated.kid, rotated.secret = "edge-b", "rotated"
	if resp := rotated.send(h); resp.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the rotated key", resp.StatusCode())
	}
}
