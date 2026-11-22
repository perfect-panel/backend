package trace

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// The otlpgrpc exporter sends spans in plaintext unless OtlpGrpcSecure asks
// for TLS: plaintext was the only behaviour before the key existed, so the
// default keeps it. The option list is what tells the two apart, since the
// exporter's client dials lazily.
func TestOtlpGrpcOptionsAreInsecureUnlessConfigured(t *testing.T) {
	insecure := otlpGrpcOptions(Config{Endpoint: "collector:4317"})
	secure := otlpGrpcOptions(Config{Endpoint: "collector:4317", OtlpGrpcSecure: true})
	if len(insecure) != len(secure)+1 {
		t.Fatalf("insecure options %d, secure %d; want exactly the WithInsecure option more by default", len(insecure), len(secure))
	}
	withHeaders := otlpGrpcOptions(Config{Endpoint: "collector:4317", OtlpHeaders: map[string]string{"x-token": "t"}})
	if len(withHeaders) != len(insecure)+1 {
		t.Fatalf("headers option missing: %d options, want %d", len(withHeaders), len(insecure)+1)
	}

	// Both configurations build an exporter; the connection is dialled on
	// the first export, so neither reaches the endpoint here.
	for name, c := range map[string]Config{
		"insecure": {Batcher: kindOtlpGrpc, Endpoint: "collector:4317"},
		"secure":   {Batcher: kindOtlpGrpc, Endpoint: "collector:4317", OtlpGrpcSecure: true},
	} {
		exporter, err := createExporter(c)
		if err != nil {
			t.Fatalf("%s: createExporter() = %v", name, err)
		}
		_ = exporter.Shutdown(context.Background())
	}
}

// The zipkin batcher still works, and whoever configures it is told it is
// deprecated.
func TestZipkinBatcherLogsItsDeprecation(t *testing.T) {
	logs := logtest.NewCollector(t)

	exporter, err := createExporter(Config{Batcher: kindZipkin, Endpoint: "http://zipkin:9411/api/v2/spans"})

	if err != nil {
		t.Fatalf("createExporter(zipkin) = %v", err)
	}
	_ = exporter.Shutdown(context.Background())
	if !strings.Contains(logs.String(), "deprecated") {
		t.Fatalf("log = %s, want the deprecation reported", logs.String())
	}
}
