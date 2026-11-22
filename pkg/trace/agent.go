// Package trace sets up OpenTelemetry tracing for the process. StartAgent
// installs the global tracer provider with the exporter the configuration
// names, and importing the package installs the W3C trace-context and
// baggage propagators, so traces continue across HTTP calls and queued
// tasks. The helpers read the trace of a request context; its trace ID is
// also the request ID the HTTP responses carry (RequestIdKey).
package trace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/perfect-panel/backend/pkg/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	// The Zipkin exporter is deprecated upstream; it stays until it is
	// removed so existing zipkin configurations keep working, and
	// createExporter warns whoever still uses it.
	"go.opentelemetry.io/otel/exporters/zipkin" //nolint:staticcheck // SA1019, see above.
)

const (
	kindJaeger   = "jaeger"
	kindZipkin   = "zipkin"
	kindOtlpGrpc = "otlpgrpc"
	kindOtlpHttp = "otlphttp"
	kindFile     = "file"
	protocolUdp  = "udp"

	// jaegerOTLPPort is the port Jaeger receives OTLP over HTTP on.
	jaegerOTLPPort = "4318"
	// otlpTracesPath is the OTLP/HTTP traces path.
	otlpTracesPath = "/v1/traces"
	// jaegerThriftPath is the path of Jaeger's legacy Thrift collector.
	jaegerThriftPath = "/api/traces"
)

var (
	agents = make(map[string]struct{})
	lock   sync.Mutex
	tp     *sdktrace.TracerProvider
)

// shutdownTimeout bounds the final export at process exit. Docker stops a
// container with SIGTERM and kills it 10 s later by default; the spans still
// queued are worth less than closing the logs before that.
var shutdownTimeout = 5 * time.Second

// StartAgent starts an opentelemetry agent.
func StartAgent(c Config) {
	if c.Disabled {
		return
	}
	logger.Info("Starting agent")
	lock.Lock()
	defer lock.Unlock()

	_, ok := agents[c.Endpoint]
	if ok {
		return
	}

	// if error happens, let later calls run.
	if err := startAgent(c); err != nil {
		return
	}

	agents[c.Endpoint] = struct{}{}
}

// StopAgent shuts down the span processors in the order they were
// registered, giving up on a collector that does not answer within
// shutdownTimeout.
func StopAgent() {
	lock.Lock()
	defer lock.Unlock()

	if tp != nil {
		// StopAgent runs at process exit, after the servers have stopped, so
		// there is no caller context to inherit.
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				logger.Errorf("[trace] shutting down the exporter timed out after %s; the spans still queued are lost", shutdownTimeout)
			} else {
				logger.Errorf("[trace] shutdown: %v", err)
			}
		}
		tp = nil
	}
	clear(agents)
}

// jaegerEndpoint maps the endpoint of the jaeger batcher onto Jaeger's
// OTLP/HTTP receiver, which Jaeger serves natively. A URL keeps its scheme,
// host and path, posting to /v1/traces when it names no path; a bare
// host:port is used as is (ok is false then, url empty). The endpoints of the
// removed Jaeger Thrift exporter do not speak OTLP: the agent (udp://host)
// and the collector's /api/traces path move to OTLP on port 4318 of the same
// host, and legacy reports that the endpoint was rewritten.
func jaegerEndpoint(endpoint string) (target string, ok, legacy bool) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, false
	}
	switch u.Scheme {
	case protocolUdp:
		return (&url.URL{Scheme: "http", Host: net.JoinHostPort(u.Hostname(), jaegerOTLPPort), Path: otlpTracesPath}).String(), true, true
	case "http", "https":
	default:
		return "", false, false
	}
	switch u.Path {
	case jaegerThriftPath:
		host := u.Host
		if u.Port() == "" || u.Port() == "14268" {
			host = net.JoinHostPort(u.Hostname(), jaegerOTLPPort)
		}
		u.Host, u.Path = host, otlpTracesPath
		return u.String(), true, true
	case "", "/":
		u.Path = otlpTracesPath
	}
	return u.String(), true, false
}

func jaegerOptions(c Config) []otlptracehttp.Option {
	var opts []otlptracehttp.Option
	target, isURL, legacy := jaegerEndpoint(c.Endpoint)
	if legacy {
		logger.Errorf("[trace] %q is a Jaeger Thrift endpoint; exporting OTLP to %s instead. Point Endpoint at Jaeger's OTLP/HTTP receiver", c.Endpoint, target)
	}
	if isURL {
		opts = append(opts, otlptracehttp.WithEndpointURL(target))
	} else {
		opts = append(opts, otlptracehttp.WithEndpoint(c.Endpoint))
		if !c.OtlpHttpSecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(c.OtlpHttpPath) > 0 {
			opts = append(opts, otlptracehttp.WithURLPath(c.OtlpHttpPath))
		}
	}
	if len(c.OtlpHeaders) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(c.OtlpHeaders))
	}
	return opts
}

// otlpGrpcOptions are the otlpgrpc exporter's options: the endpoint, the
// headers, and plaintext unless OtlpGrpcSecure asks for TLS, which is then
// verified against the system roots.
func otlpGrpcOptions(c Config) []otlptracegrpc.Option {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(c.Endpoint)}
	if !c.OtlpGrpcSecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if len(c.OtlpHeaders) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(c.OtlpHeaders))
	}
	return opts
}

// createExporter builds the exporter c names. The exporter lives as long as
// the process, beyond any caller, so the OTLP exporters are built with a root
// context, which New only uses to start the client.
func createExporter(c Config) (sdktrace.SpanExporter, error) {
	switch c.Batcher {
	case kindJaeger:
		// Jaeger ingests OTLP natively; the deprecated Jaeger exporter is
		// gone, so this batcher exports OTLP over HTTP to Jaeger.
		return otlptracehttp.New(context.Background(), jaegerOptions(c)...)
	case kindZipkin:
		logger.Errorf("[trace] the zipkin batcher is deprecated and will be removed with the upstream exporter; export with otlphttp or otlpgrpc (an OpenTelemetry Collector can forward to Zipkin)")
		return zipkin.New(c.Endpoint)
	case kindOtlpGrpc:
		// Always treat trace exporter as optional component, so we use nonblock here,
		// otherwise this would slow down app start up even set a dial timeout here when
		// endpoint can not reach.
		// If the connection not dial success, the global otel ErrorHandler will catch error
		// when reporting data like other exporters.
		return otlptracegrpc.New(context.Background(), otlpGrpcOptions(c)...)
	case kindOtlpHttp:
		// Not support flexible configuration now.
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(c.Endpoint),
		}

		if !c.OtlpHttpSecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(c.OtlpHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(c.OtlpHeaders))
		}
		if len(c.OtlpHttpPath) > 0 {
			opts = append(opts, otlptracehttp.WithURLPath(c.OtlpHttpPath))
		}
		return otlptracehttp.New(context.Background(), opts...)
	case kindFile:
		f, err := os.OpenFile(c.Endpoint, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("file exporter endpoint: %w", err)
		}
		if err := os.Chmod(c.Endpoint, 0o600); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("secure file exporter endpoint: %w", err)
		}
		return stdouttrace.New(stdouttrace.WithWriter(f))
	default:
		return nil, fmt.Errorf("unknown exporter: %s", c.Batcher)
	}
}

func startAgent(c Config) error {
	AddResources(semconv.ServiceNameKey.String(c.Name))
	sampler := c.Sampler
	// Without an exporter there is no trace destination. Keep propagation and
	// request IDs, but avoid recording every span in memory.
	if strings.TrimSpace(c.Endpoint) == "" {
		sampler = 0
	}
	if sampler < 0 {
		sampler = 0
	} else if sampler > 1 {
		sampler = 1
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampler))),
		// Record information about this application in a Resource.
		sdktrace.WithResource(resource.NewSchemaless(attrResources...)),
	}

	if len(c.Endpoint) > 0 {
		exp, err := createExporter(c)
		if err != nil {
			logger.Error(err)
			return err
		}

		// Always be sure to batch in production.
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp = sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Errorf("[otel] error: %v", err)
	}))

	return nil
}
