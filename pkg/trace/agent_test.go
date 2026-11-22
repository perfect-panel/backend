package trace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// hangingExporter never finishes an export or its shutdown until the
// context ends, like a collector that accepted the connection and stopped
// answering.
type hangingExporter struct{}

func (hangingExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	<-ctx.Done()
	return ctx.Err()
}

func (hangingExporter) Shutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// StopAgent runs at process exit, inside Docker's default stop grace of 10 s:
// a hanging collector must not hold it past that, or the process is killed
// before the logs are closed. The give-up is logged.
func TestStopAgentGivesUpOnAHangingExporter(t *testing.T) {
	logs := logtest.NewCollector(t)
	previous := shutdownTimeout
	shutdownTimeout = 300 * time.Millisecond
	t.Cleanup(func() { shutdownTimeout = previous })
	lock.Lock()
	tp = sdktrace.NewTracerProvider(sdktrace.WithBatcher(hangingExporter{}))
	lock.Unlock()
	_, span := tp.Tracer("test").Start(context.Background(), "pending")
	span.End()

	start := time.Now()
	StopAgent()

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("StopAgent took %s with a hanging exporter, want it to give up at the shutdown timeout", elapsed)
	}
	if !strings.Contains(logs.String(), "timed out") {
		t.Fatalf("logs = %q, want the shutdown timeout reported", logs.String())
	}
}

// The jaeger batcher exports OTLP over HTTP: Jaeger receives it natively.
func TestJaegerBatcherExportsOTLPOverHTTP(t *testing.T) {
	var mu sync.Mutex
	var paths, contentTypes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	StartAgent(Config{Name: "jaeger-otlp", Endpoint: server.URL, Batcher: kindJaeger, Sampler: 1})
	_, span := otel.Tracer(TraceName).Start(context.Background(), "exported")
	span.End()
	StopAgent()

	mu.Lock()
	defer mu.Unlock()
	if len(paths) == 0 || paths[0] != "/v1/traces" || contentTypes[0] != "application/x-protobuf" {
		t.Fatalf("requests = %v (%v), want an OTLP/HTTP export to /v1/traces", paths, contentTypes)
	}
}

func TestJaegerEndpointMapsOntoOTLP(t *testing.T) {
	for _, tt := range []struct {
		endpoint, target string
		isURL, legacy    bool
	}{
		{"jaeger:4318", "", false, false},
		{"http://jaeger:4318", "http://jaeger:4318/v1/traces", true, false},
		{"https://traces.example.com/", "https://traces.example.com/v1/traces", true, false},
		{"http://jaeger:4318/custom/path", "http://jaeger:4318/custom/path", true, false},
		{"udp://agent:6831", "http://agent:4318/v1/traces", true, true},
		{"http://collector:14268/api/traces", "http://collector:4318/v1/traces", true, true},
		{"http://collector/api/traces", "http://collector:4318/v1/traces", true, true},
		{"http://collector:9999/api/traces", "http://collector:9999/v1/traces", true, true},
	} {
		target, isURL, legacy := jaegerEndpoint(tt.endpoint)
		if target != tt.target || isURL != tt.isURL || legacy != tt.legacy {
			t.Fatalf("jaegerEndpoint(%q) = (%q, %v, %v), want (%q, %v, %v)", tt.endpoint, target, isURL, legacy, tt.target, tt.isURL, tt.legacy)
		}
	}
}

func TestStartAgent(t *testing.T) {
	// Discard rather than Disable: Disable silences the process logger for
	// good, which would also blind the collectors of the tests after this one.
	logtest.Discard(t)

	const (
		endpoint1  = "localhost:1234"
		endpoint2  = "remotehost:1234"
		endpoint3  = "localhost:1235"
		endpoint4  = "localhost:1236"
		endpoint5  = "udp://localhost:6831"
		endpoint6  = "localhost:1237"
		endpoint71 = "/tmp/trace.log"
		endpoint72 = "/not-exist-fs/trace.log"
	)
	c1 := Config{
		Name: "foo",
	}
	c2 := Config{
		Name:     "bar",
		Endpoint: endpoint1,
		Batcher:  kindJaeger,
	}
	c3 := Config{
		Name:     "any",
		Endpoint: endpoint2,
		Batcher:  kindZipkin,
	}
	c4 := Config{
		Name:     "bla",
		Endpoint: endpoint3,
		Batcher:  "otlp",
	}
	c5 := Config{
		Name:     "otlpgrpc",
		Endpoint: endpoint3,
		Batcher:  kindOtlpGrpc,
		OtlpHeaders: map[string]string{
			"uptrace-dsn": "http://project2_secret_token@localhost:14317/2",
		},
	}
	c6 := Config{
		Name:     "otlphttp",
		Endpoint: endpoint4,
		Batcher:  kindOtlpHttp,
		OtlpHeaders: map[string]string{
			"uptrace-dsn": "http://project2_secret_token@localhost:14318/2",
		},
		OtlpHttpPath: "/v1/traces",
	}
	c7 := Config{
		Name:     "UDP",
		Endpoint: endpoint5,
		Batcher:  kindJaeger,
	}
	c8 := Config{
		Disabled: true,
		Endpoint: endpoint6,
		Batcher:  kindJaeger,
	}
	c9 := Config{
		Name:     "file",
		Endpoint: endpoint71,
		Batcher:  kindFile,
	}
	c10 := Config{
		Name:     "file",
		Endpoint: endpoint72,
		Batcher:  kindFile,
	}

	StartAgent(c1)
	StartAgent(c1)
	StartAgent(c2)
	StartAgent(c3)
	StartAgent(c4)
	StartAgent(c5)
	StartAgent(c6)
	StartAgent(c7)
	StartAgent(c8)
	StartAgent(c9)
	StartAgent(c10)
	defer StopAgent()

	lock.Lock()
	defer lock.Unlock()

	// because remotehost cannot be resolved
	assert.Equal(t, 6, len(agents))
	_, ok := agents[""]
	assert.True(t, ok)
	_, ok = agents[endpoint1]
	assert.True(t, ok)
	_, ok = agents[endpoint2]
	assert.False(t, ok)
	_, ok = agents[endpoint5]
	assert.True(t, ok)
	_, ok = agents[endpoint6]
	assert.False(t, ok)
	_, ok = agents[endpoint71]
	assert.True(t, ok)
	_, ok = agents[endpoint72]
	assert.False(t, ok)
}
