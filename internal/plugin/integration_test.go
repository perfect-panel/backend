package plugin

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/perfect-panel/server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

// Mock Redis for the test
type mockRedis struct {
	data map[string]string
	mu   sync.RWMutex
}

func (m *mockRedis) Get(key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if val, ok := m.data[key]; ok {
		return val, nil
	}
	return "", nil
}

func (m *mockRedis) Set(key, value string, ttlSeconds int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

// Mock Store for the test
type mockStore struct{}

func (m *mockStore) Query(model string, operation string, conditions map[string]interface{}, fields []string, limit, offset int32) ([]map[string]interface{}, int64, error) {
	if model == "user" && operation == "find" {
		if status, ok := conditions["status"].(string); ok && status == "active" {
			return []map[string]interface{}{
				{"id": 1, "username": "alice", "status": "active"},
				{"id": 2, "username": "bob", "status": "active"},
			}, 2, nil
		}
	}
	return nil, 0, nil
}

func (m *mockRedis) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

func TestDemoPluginIntegration(t *testing.T) {
	cfg := config.Config{
		Plugin: config.PluginConfig{
			Enabled: true,
		},
		Site: config.SiteConfig{
			SiteName: "UnitTestPanel",
		},
	}

	mockRds := &mockRedis{data: make(map[string]string)}
	env := &HostEnv{
		Config: cfg,
		Redis:  mockRds,
		Store:  &mockStore{},
		Queue:  nil,
	}

	mgr := NewManager(env)
	defer mgr.Stop()

	ctx := context.Background()
	pluginDir := prepareDemoPlugin(t)
	manifest, err := ParseManifest(pluginDir)
	require.NoError(t, err)

	mgr.loadPlugin(ctx, pluginDir, manifest)
	instance := mgr.GetPlugin("demo-plugin")
	require.NotNil(t, instance)
	require.Equal(t, StatusRunning, instance.GetStatus(), "plugin error: %s", instance.Error)

	t.Run("InitRegistersRoutesAndMiddleware", func(t *testing.T) {
		assertRouteRegistered(t, mgr, "demo-plugin", "GET", "/hello", "handle_hello")
		assertRouteRegistered(t, mgr, "demo-plugin", "POST", "/echo", "handle_echo")
		assertRouteRegistered(t, mgr, "demo-plugin", "GET", "/redis/counter", "handle_redis_counter")
		assertRouteRegistered(t, mgr, "demo-plugin", "GET", "/async/redis", "handle_async_redis")
		assertRouteRegistered(t, mgr, "demo-plugin", "GET", "/db/users", "handle_db_users")
		assertRouteRegistered(t, mgr, "demo-plugin", "GET", "/guarded", "handle_guarded")

		mw, ok := mgr.FindMiddleware("demo-plugin", "demo_guard")
		require.True(t, ok)
		assert.Equal(t, "mw_demo_guard", mw.Handler)
	})

	t.Run("HelloHandlerUsesQueryContextAndConfig", func(t *testing.T) {
		req := &pluginv1.HandleRequest{
			Method: "GET",
			Path:   "/v1/plugin/demo-plugin/hello",
			Query: map[string]*pluginv1.StringList{
				"name": {Values: []string{"Alice"}},
			},
			Context: &pluginv1.RequestContext{UserId: 1001},
		}
		res, err := mgr.CallPlugin(ctx, "demo-plugin", "handle_hello", req)
		require.NoError(t, err)
		assert.Equal(t, int32(200), res.Status)
		assert.Contains(t, string(res.Body), `"message":"你好，Alice"`)
		assert.Contains(t, string(res.Body), `"site":"UnitTestPanel"`)
		assert.Contains(t, string(res.Body), `"user_id":1001`)
	})

	t.Run("EchoHandlerReturnsBody", func(t *testing.T) {
		req := &pluginv1.HandleRequest{
			Method: "POST",
			Path:   "/v1/plugin/demo-plugin/echo",
			Body:   []byte("hello wasm"),
		}
		res, err := mgr.CallPlugin(ctx, "demo-plugin", "handle_echo", req)
		require.NoError(t, err)
		assert.Equal(t, int32(200), res.Status)
		assert.Contains(t, string(res.Body), `"body":"hello wasm"`)
	})

	t.Run("RedisCounterHandlerUsesPluginNamespace", func(t *testing.T) {
		req := &pluginv1.HandleRequest{
			Method: "GET",
			Path:   "/v1/plugin/demo-plugin/redis/counter",
		}
		res, err := mgr.CallPlugin(ctx, "demo-plugin", "handle_redis_counter", req)
		require.NoError(t, err)
		assert.Equal(t, int32(200), res.Status)
		assert.Contains(t, string(res.Body), `"counter":1`)

		res, err = mgr.CallPlugin(ctx, "demo-plugin", "handle_redis_counter", req)
		require.NoError(t, err)
		assert.Contains(t, string(res.Body), `"counter":2`)
		counter, err := mockRds.Get("plugin:demo-plugin:counter")
		require.NoError(t, err)
		assert.Equal(t, "2", counter)
	})

	t.Run("AsyncRedisHandlerUsesHostGoroutinePool", func(t *testing.T) {
		req := &pluginv1.HandleRequest{
			Method: "GET",
			Path:   "/v1/plugin/demo-plugin/async/redis",
		}
		res, err := mgr.CallPlugin(ctx, "demo-plugin", "handle_async_redis", req)
		require.NoError(t, err)
		assert.Equal(t, int32(200), res.Status)
		assert.Contains(t, string(res.Body), `"async":true`)
		assert.Contains(t, string(res.Body), `"value":"hello from async"`)
		asyncMessage, err := mockRds.Get("plugin:demo-plugin:async_message")
		require.NoError(t, err)
		assert.Equal(t, "hello from async", asyncMessage)
	})

	t.Run("DBHandlerQueriesActiveUsers", func(t *testing.T) {
		req := &pluginv1.HandleRequest{
			Method: "GET",
			Path:   "/v1/plugin/demo-plugin/db/users",
		}
		res, err := mgr.CallPlugin(ctx, "demo-plugin", "handle_db_users", req)
		require.NoError(t, err)
		assert.Equal(t, int32(200), res.Status)
		assert.Contains(t, string(res.Body), `"rows":2`)
		assert.Contains(t, string(res.Body), `"total":2`)
	})

	t.Run("CustomMiddlewareAbortAndNext", func(t *testing.T) {
		req := &pluginv1.HandleRequest{
			Method: "GET",
			Path:   "/v1/plugin/demo-plugin/guarded",
		}
		res, err := mgr.CallPluginMiddleware(ctx, "demo-plugin", "mw_demo_guard", req)
		require.NoError(t, err)
		assert.Equal(t, "abort", res.Action)
		assert.Equal(t, int32(403), res.Status)

		req.Headers = map[string]*pluginv1.StringList{
			"X-Demo-Token": {Values: []string{"let-me-in"}},
		}
		res, err = mgr.CallPluginMiddleware(ctx, "demo-plugin", "mw_demo_guard", req)
		require.NoError(t, err)
		assert.Equal(t, "next", res.Action)
		assert.Equal(t, "passed", res.Headers["x-demo-guard"])
	})

	t.Run("EventSubscriptionInvokesDemoHandler", func(t *testing.T) {
		payload, err := structpb.NewStruct(map[string]interface{}{"message": "pong"})
		require.NoError(t, err)

		mgr.EventBus().Publish("demo.ping", payload)
		require.Eventually(t, func() bool {
			lastEvent, err := mockRds.Get("plugin:demo-plugin:last_event")
			return err == nil && lastEvent == "pong"
		}, time.Second, 10*time.Millisecond)
	})
}

func prepareDemoPlugin(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)
	sdkDir := filepath.Clean(filepath.Join(wd, "..", "..", "..", "ppanel-sdk"))
	if _, err := os.Stat(filepath.Join(sdkDir, "Cargo.toml")); err != nil {
		t.Skipf("ppanel-sdk workspace not found at %s", sdkDir)
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required to build the SDK demo plugin")
	}

	cmd := exec.Command("cargo", "build", "-p", "demo-plugin", "--target", "wasm32-wasip1", "--release")
	cmd.Dir = sdkDir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Run(), output.String())

	pluginDir := filepath.Join(t.TempDir(), "demo-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	copyFile(t, filepath.Join(sdkDir, "examples", "demo-plugin", "plugin.yaml"), filepath.Join(pluginDir, "plugin.yaml"))
	copyFile(t, filepath.Join(sdkDir, "target", "wasm32-wasip1", "release", "demo_plugin.wasm"), filepath.Join(pluginDir, "plugin.wasm"))
	return pluginDir
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0644))
}

func assertRouteRegistered(t *testing.T, mgr *Manager, pluginName, method, path, handler string) {
	t.Helper()
	route, ok := mgr.FindRoute(pluginName, method, path)
	require.True(t, ok, "%s %s should be registered", method, path)
	assert.Equal(t, handler, route.Handler)
}
