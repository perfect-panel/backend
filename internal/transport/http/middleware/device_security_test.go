package middleware

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/auth/deviceauth"
	appconfig "github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/redis/go-redis/v9"
)

func deviceReplayClient(t *testing.T) *redis.Client {
	t.Helper()
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestDeviceSecurityRejectsUnsignedAndReplayedRequests(t *testing.T) {
	const path = "/v1/auth/login/device"
	const secret = "test-only-transport-key"
	client := deviceReplayClient(t)
	router := server.New()
	calls := 0
	router.POST(path, DeviceMiddleware(func() appconfig.DeviceConfig {
		return appconfig.DeviceConfig{Enable: true, EnableSecurity: true, OnlyRealDevice: true, SecuritySecret: secret}
	}, client), func(_ context.Context, c *app.RequestContext) {
		calls++
		c.JSON(200, map[string]any{"data": map[string]string{"ok": "true"}})
	})
	data, timestamp, err := deviceauth.Encrypt([]byte(`{"identifier":"device"}`), secret)
	if err != nil {
		t.Fatal(err)
	}
	envelope := deviceauth.Envelope{Data: data, Time: timestamp, Sign: deviceauth.Sign(secret, "POST", path, "body", data, timestamp)}
	body, _ := json.Marshal(envelope)
	unsignedBody, _ := json.Marshal(map[string]string{"data": data, "time": timestamp})
	for _, tc := range []struct {
		name, query string
		body        []byte
		wantCalls   int
	}{
		{"empty request", "", nil, 0},
		{"plaintext query", "identifier=device&user_agent=spoof", nil, 0},
		{"invalid query ciphertext", url.Values{"data": {"bad"}, "time": {timestamp}, "sign": {envelope.Sign}}.Encode(), nil, 0},
		{"unsigned envelope", "", unsignedBody, 0},
		{"unsigned query beside valid body", "identifier=other", body, 0},
		{"signed body", "", body, 1},
		{"replayed body", "", body, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := router.NewContext()
			c.Request.Header.SetMethod("POST")
			c.Request.SetRequestURI(path + "?" + tc.query)
			c.Request.SetBody(tc.body)
			router.ServeHTTP(context.Background(), c)
			if calls != tc.wantCalls {
				t.Fatalf("downstream calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func TestDeviceSecurityUsesAuthenticatedSessionWithoutHeader(t *testing.T) {
	router := server.New()
	called := false
	router.POST("/protected", func(ctx context.Context, c *app.RequestContext) {
		ctx = user.NewContext(ctx, &user.User{Id: 1})
		ctx = context.WithValue(ctx, requestctx.LoginType, "device")
		c.Next(ctx)
	}, DeviceMiddleware(func() appconfig.DeviceConfig {
		return appconfig.DeviceConfig{Enable: true, EnableSecurity: true, SecuritySecret: "key"}
	}, deviceReplayClient(t)), func(context.Context, *app.RequestContext) { called = true })
	c := router.NewContext()
	c.Request.Header.SetMethod("POST")
	c.Request.SetRequestURI("/protected")
	c.Request.SetBodyString(`{"plaintext":"must not be accepted"}`)
	router.ServeHTTP(context.Background(), c)
	if called {
		t.Fatal("omitting Login-Type bypassed the signed session's device transport")
	}
}

func TestDeviceSecurityRequiresReplayStoreAndValidQueryEnvelope(t *testing.T) {
	const secret = "key"
	data, timestamp, err := deviceauth.Encrypt([]byte(`{"identifier":"signed-device"}`), secret)
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"data": {data}, "time": {timestamp}, "sign": {deviceauth.Sign(secret, "POST", "/device", "query", data, timestamp)}}
	makeRequest := func() *app.RequestContext {
		c := app.NewContext(0)
		c.Request.Header.SetMethod("POST")
		c.Request.SetRequestURI("/device?" + query.Encode())
		return c
	}
	if err := DecryptDeviceRequest(context.Background(), makeRequest(), secret, nil); err == nil {
		t.Fatal("nil replay store accepted")
	}
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	rdb.SetError("unavailable")
	if err := DecryptDeviceRequest(context.Background(), makeRequest(), secret, client); err == nil {
		t.Fatal("Redis error accepted")
	}
	rdb.SetError("")
	c := makeRequest()
	if err := DecryptDeviceRequest(context.Background(), c, secret, client); err != nil {
		t.Fatal(err)
	}
	if c.Query("identifier") != "signed-device" || c.QueryArgs().Len() != 1 {
		t.Fatal("query was not replaced by its authenticated contents")
	}
	if err := DecryptDeviceRequest(context.Background(), makeRequest(), secret, client); err == nil {
		t.Fatal("query replay accepted")
	}
}
