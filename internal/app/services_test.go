package app

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/app/bootstrap"
	"github.com/perfect-panel/backend/internal/app/lifecycle"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/geoip"
	httpserver "github.com/perfect-panel/backend/internal/transport/http/server"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// optionalDependencies are the dependency fields the application leaves nil
// on purpose, each with the reason.
var optionalDependencies = map[string]string{
	// The task worker's aggregator only flushes buckets; these apply to
	// accepting a report, which the node API does with its own aggregator.
	"tasks.Traffic.Aggregator.TrafficReportThreshold": "report-time only",
	"tasks.Traffic.Aggregator.Multiplier":             "report-time only",
	"tasks.Traffic.Aggregator.ServedSubscriptions":    "report-time only",
}

// The composition root wires every port: a dependency left nil would only
// surface as a panic when the first request or task reached it. The graph is
// assembled on in-memory infrastructure, so nothing connects anywhere.
func TestAssembledDependenciesAreComplete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	var c config.Config
	c.Redis.Host = mini.Addr()
	queue, inspector := NewAsynqClient(c), NewAsynqInspector(c)
	t.Cleanup(func() {
		_ = queue.Close()
		_ = inspector.Close()
	})

	srv := assemble(c, db, rds, &geoip.IPLocation{}, queue, inspector)
	bootstrapped := lifecycle.NewReadiness()
	service := srv.serviceDependencies(bootstrapped)
	for name, deps := range map[string]any{
		"application": *srv,
		"service":     service,
		"bootstrap":   *service.Bootstrap,
		"http":        service.HTTP(),
		"tasks":       srv.taskDependencies(bootstrapped),
	} {
		for _, field := range unwired(reflect.ValueOf(deps), name) {
			if _, optional := optionalDependencies[field]; !optional {
				t.Errorf("%s is not wired", field)
			}
		}
	}
}

// unwired lists the nil dependency fields of v, a struct, named path. It
// descends into nested structs, not into what pointers and interfaces hold.
func unwired(v reflect.Value, path string) []string {
	var nils []string
	for i := 0; i < v.NumField(); i++ {
		field, name := v.Field(i), path+"."+v.Type().Field(i).Name
		switch field.Kind() {
		case reflect.Struct:
			if strings.HasPrefix(field.Type().PkgPath(), "github.com/perfect-panel/backend/") && !isConfigValue(field.Type()) {
				nils = append(nils, unwired(field, name)...)
			}
		case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Chan:
			if field.IsNil() {
				nils = append(nils, strings.TrimPrefix(name, "http."))
			}
		}
	}
	return nils
}

// isConfigValue reports whether t is configuration data rather than a set of
// dependencies: its zero fields are settings, not missing wiring.
func isConfigValue(t reflect.Type) bool {
	return t.PkgPath() == "github.com/perfect-panel/backend/internal/config" || strings.HasSuffix(fmt.Sprint(t), "Snapshot")
}

// The process stops the HTTP server first, so no new request arrives while
// the workers drain, and flushes the trace exporter last, once every service
// produced its final spans. The order used to be scheduler, worker, HTTP,
// with the exporter flushed inside the HTTP service's stop.
func TestServicesStopTheHTTPServerFirstAndFlushTracesLast(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	var c config.Config
	c.Redis.Host = mini.Addr()
	queue, inspector := NewAsynqClient(c), NewAsynqInspector(c)
	t.Cleanup(func() {
		_ = queue.Close()
		_ = inspector.Close()
	})
	srv := assemble(c, db, rds, &geoip.IPLocation{}, queue, inspector)

	order := srv.services(c).StopOrder()

	var names []string
	for _, service := range order {
		names = append(names, fmt.Sprintf("%T", service))
	}
	want := []string{"*app.Service", "*scheduler.Service", "*task.Service", "lifecycle.stopOnlyService"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("stop order = %v, want %v", names, want)
	}
	// The readiness probes reach the assembled connections.
	probes := srv.serviceDependencies(lifecycle.NewReadiness()).Probes
	if len(probes) != 2 {
		t.Fatalf("probes = %d, want the database and Redis", len(probes))
	}
	for _, probe := range probes {
		if err := probe.Ping(context.Background()); err != nil {
			t.Fatalf("%s probe: %v", probe.Name, err)
		}
	}
	mini.Close()
	if err := probes[1].Ping(context.Background()); err == nil {
		t.Fatal("the Redis probe answered after Redis went away")
	}
}

// The task worker waits for the HTTP service's bootstrap. A bootstrap that
// fails is reported to it before the HTTP service gives up, so nothing waits
// on a process that is going down.
func TestHTTPServiceReportsAFailedBootstrap(t *testing.T) {
	logtest.Discard(t)
	bootstrapped := lifecycle.NewReadiness()
	var c config.Config
	// A driver the migration does not support fails the bootstrap before
	// anything connects.
	c.SetDatabaseConfig(orm.Config{Driver: "sqlite", Dbname: "ppanel"})
	current := func() config.Config { return c }
	svc := NewService(Dependencies{
		Config:       current,
		Bootstrap:    &bootstrap.Dependencies{Config: current},
		HTTP:         func() httpserver.Dependencies { return httpserver.Dependencies{} },
		Bootstrapped: bootstrapped,
	})

	func() {
		defer func() {
			if recover() == nil {
				t.Error("Start went on although the bootstrap failed")
			}
		}()
		svc.Start()
	}()

	select {
	case <-bootstrapped.Done():
	default:
		t.Fatal("the bootstrap's failure was not signalled")
	}
	if bootstrapped.Err() == nil {
		t.Fatal("the signal reports success after a failed bootstrap")
	}
}
