package routes_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	httpserver "github.com/perfect-panel/backend/internal/transport/http/server"

	"github.com/cloudwego/hertz/pkg/app/server"
	billingHTTP "github.com/perfect-panel/backend/internal/module/billing/transport/http"
	notificationHTTP "github.com/perfect-panel/backend/internal/module/notification/transport/http"
	"github.com/perfect-panel/backend/internal/transport/http/routes"
)

type swaggerDocument struct {
	Paths               map[string]map[string]swaggerOperation `json:"paths"`
	Definitions         map[string]json.RawMessage             `json:"definitions"`
	SecurityDefinitions map[string]json.RawMessage             `json:"securityDefinitions"`
}

type swaggerOperation struct {
	Summary    string                     `json:"summary"`
	Tags       []string                   `json:"tags"`
	Responses  map[string]json.RawMessage `json:"responses"`
	Parameters []swaggerParameter         `json:"parameters"`
	Security   []map[string][]string      `json:"security"`
}

type swaggerParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
}

func TestSwaggerCoversHertzRoutes(t *testing.T) {
	deps := routes.Dependencies{}
	// Register both supported subscription URL forms. SubscribePath remains
	// empty so that the documented default path is registered as well.
	deps.Config.Subscribe.PanDomain = true
	// The Edge route is opt-in in production, but it is part of the complete
	// documented API surface and is enabled for this contract test.
	deps.Config.EdgeSubscribe.Enabled = true

	engine := server.New()
	routes.RegisterHandlers(engine, deps)
	notificationHTTP.RegisterTelegramHandlers(engine, nil, func() string { return "" })
	billingHTTP.RegisterNotifyHandlers(engine, deps.Billing)
	// The health endpoints are registered by the composition root, next to
	// the routes; they are part of the documented surface.
	httpserver.RegisterHealthHandlers(engine, httpserver.HealthDependencies{})

	document := readSwaggerDocument(t)
	want := make(map[string]bool)
	for _, registered := range engine.Routes() {
		want[routeKey(registered.Method, normalizeHertzPath(registered.Path))] = true
	}

	got := make(map[string]bool)
	for routePath, methods := range document.Paths {
		for method, operation := range methods {
			method = strings.ToUpper(method)
			if !isDocumentedHTTPMethod(method) {
				continue
			}
			key := routeKey(method, routePath)
			got[key] = true
			if strings.TrimSpace(operation.Summary) == "" {
				t.Errorf("%s has no summary", key)
			}
			if len(operation.Tags) == 0 {
				t.Errorf("%s has no tags", key)
			}
			if len(operation.Responses) == 0 {
				t.Errorf("%s has no responses", key)
			}
			validatePathParameters(t, key, routePath, operation.Parameters)
			for _, requirements := range operation.Security {
				for name := range requirements {
					if _, ok := document.SecurityDefinitions[name]; !ok {
						t.Errorf("%s references undefined security scheme %q", key, name)
					}
				}
			}
		}
	}
	validateLocalReferences(t, document)

	var missing, stale []string
	for key := range want {
		if !got[key] {
			missing = append(missing, key)
		}
	}
	for key := range got {
		if !want[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		t.Fatalf("Swagger route mismatch\nmissing from Swagger: %v\nnot registered by Hertz: %v", missing, stale)
	}
}

func TestSwaggerScopesPartitionFullDocument(t *testing.T) {
	root := projectRoot(t)
	full := readSwaggerDocumentFile(t, filepath.Join(root, "ppanel.json"))
	fullOperations := documentedOperations(full)
	combined := make(map[string]bool)

	for _, scope := range []string{"admin", "user", "common", "node", "edge"} {
		file := filepath.Join(root, "build", "swagger", scope+".json")
		if _, err := os.Stat(file); err != nil {
			if os.IsNotExist(err) {
				t.Skip("generated scoped Swagger documents are not present; run script/generate-swagger.sh")
			}
			t.Fatalf("stat scoped Swagger document: %v", err)
		}
		document := readSwaggerDocumentFile(t, file)
		validateLocalReferences(t, document)
		for routePath, methods := range document.Paths {
			for method, operation := range methods {
				method = strings.ToUpper(method)
				if !isDocumentedHTTPMethod(method) {
					continue
				}
				key := routeKey(method, routePath)
				if len(operation.Tags) != 1 || operation.Tags[0] != scope {
					t.Errorf("%s scope contains %s with tags %v", scope, key, operation.Tags)
				}
				if combined[key] {
					t.Errorf("%s appears in more than one Swagger scope", key)
				}
				combined[key] = true
			}
		}
	}

	var missing, extra []string
	for key := range fullOperations {
		if !combined[key] {
			missing = append(missing, key)
		}
	}
	for key := range combined {
		if !fullOperations[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("scoped Swagger documents do not partition the full document\nmissing: %v\nextra: %v", missing, extra)
	}
}

func TestSwaggerKeepsStableDTOModelNames(t *testing.T) {
	document := readSwaggerDocument(t)
	for name := range document.Definitions {
		if strings.Contains(name, "contract.") || strings.Contains(name, "internal_module_") {
			t.Errorf("unstable module-qualified Swagger definition %q", name)
		}
	}
	for _, name := range []string{
		"dto.BalanceLog",
		"dto.PlatformResponse",
		"dto.Protocol",
		"dto.Subscribe",
		"dto.User",
	} {
		if _, ok := document.Definitions[name]; !ok {
			t.Errorf("stable Swagger definition %q is missing", name)
		}
	}
}

func validatePathParameters(t *testing.T, key, routePath string, parameters []swaggerParameter) {
	t.Helper()
	for _, part := range strings.Split(routePath, "/") {
		if !strings.HasPrefix(part, "{") || !strings.HasSuffix(part, "}") {
			continue
		}
		name := strings.Trim(part, "{}")
		found := false
		for _, parameter := range parameters {
			if parameter.Name == name && parameter.In == "path" && parameter.Required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s does not declare required path parameter %q", key, name)
		}
	}
}

func validateLocalReferences(t *testing.T, document swaggerDocument) {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal Swagger document for reference validation: %v", err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("decode Swagger document for reference validation: %v", err)
	}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if reference, ok := typed["$ref"].(string); ok {
				const prefix = "#/definitions/"
				if strings.HasPrefix(reference, prefix) {
					name := strings.TrimPrefix(reference, prefix)
					if _, exists := document.Definitions[name]; !exists {
						t.Errorf("Swagger references undefined definition %q", name)
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
}

func readSwaggerDocument(t *testing.T) swaggerDocument {
	t.Helper()
	return readSwaggerDocumentFile(t, filepath.Join(projectRoot(t), "ppanel.json"))
}

func readSwaggerDocumentFile(t *testing.T, file string) swaggerDocument {
	t.Helper()
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read generated Swagger document: %v", err)
	}
	var document swaggerDocument
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("parse generated Swagger document: %v", err)
	}
	return document
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", ".."))
}

func documentedOperations(document swaggerDocument) map[string]bool {
	result := make(map[string]bool)
	for routePath, methods := range document.Paths {
		for method := range methods {
			method = strings.ToUpper(method)
			if isDocumentedHTTPMethod(method) {
				result[routeKey(method, routePath)] = true
			}
		}
	}
	return result
}

func normalizeHertzPath(value string) string {
	parts := strings.Split(value, "/")
	for index, part := range parts {
		if strings.HasPrefix(part, ":") || strings.HasPrefix(part, "*") {
			parts[index] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func routeKey(method, routePath string) string {
	return fmt.Sprintf("%s %s", strings.ToUpper(method), routePath)
}

func isDocumentedHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}
