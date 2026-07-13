package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/xerr"
)

var routeHandlerName = regexp.MustCompile(`^(.*)\.[^.]+\.([^.]+)\.func[0-9]+$`)

func TestRegisterHandlers_routeInventory(t *testing.T) {
	// Given
	router := server.Default()
	RegisterHandlers(router, &svc.ServiceContext{})
	routes := router.Routes()
	var actual strings.Builder
	for _, route := range routes {
		logicalHandler, err := normalizeRouteHandler(route.Handler)
		if err != nil {
			t.Fatalf("normalize route %s %s handler %q: %v", route.Method, route.Path, route.Handler, err)
		}
		actual.WriteString(route.Method)
		actual.WriteByte(' ')
		actual.WriteString(route.Path)
		actual.WriteByte(' ')
		actual.WriteString(logicalHandler)
		actual.WriteByte('\n')
	}

	// When
	expected, err := os.ReadFile("testdata/routes.golden")
	if err != nil {
		t.Fatalf("read route golden: %v", err)
	}

	// Then
	if len(routes) != 259 {
		t.Fatalf("expected 259 routes, got %d", len(routes))
	}
	if !strings.Contains(actual.String(), "GET /v1/admin/plugins ") || !strings.Contains(actual.String(), "GET /v1/admin/plugins/ ") {
		t.Fatal("expected distinct plugin collection slash variants")
	}
	if !bytes.Equal([]byte(actual.String()), expected) {
		t.Fatal("route inventory differs from golden")
	}
}

func normalizeRouteHandler(raw string) (string, error) {
	matches := routeHandlerName.FindStringSubmatch(raw)
	if matches == nil {
		return "", errors.New("unsupported Hertz closure name")
	}

	return matches[1] + "." + matches[2], nil
}

func TestRegisterHandlers_rejectsPluginRequestWithoutAuthorization(t *testing.T) {
	// Given
	router := server.Default()
	RegisterHandlers(router, &svc.ServiceContext{})
	ctx := router.NewContext()
	ctx.Request.SetRequestURI("/v1/admin/plugin/list")
	ctx.Request.Header.SetMethod(http.MethodGet)

	// When
	router.ServeHTTP(context.Background(), ctx)

	// Then
	var response struct {
		Code uint32 `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal auth envelope: %v", err)
	}
	if response.Code != xerr.ErrorTokenEmpty || response.Msg != "User token is empty" {
		t.Fatalf("expected missing-token envelope, got (%d, %q)", response.Code, response.Msg)
	}
}

func TestRegisterHandlers_configuredRoutes(t *testing.T) {
	routeCases := []struct {
		name           string
		subscribe      appconfig.SubscribeConfig
		wantRouteCount int
		present        []string
		absent         []string
	}{
		{
			name:           "empty-fallback",
			wantRouteCount: 259,
			present:        []string{"/v1/subscribe/config"},
			absent:         []string{"/"},
		},
		{
			name: "custom-path-without-fallback",
			subscribe: appconfig.SubscribeConfig{
				SubscribePath: "/custom/subscribe",
			},
			wantRouteCount: 259,
			present:        []string{"/custom/subscribe"},
			absent:         []string{"/v1/subscribe/config", "/"},
		},
		{
			name: "pan-domain-disabled",
			subscribe: appconfig.SubscribeConfig{
				PanDomain: false,
			},
			wantRouteCount: 259,
			present:        []string{"/v1/subscribe/config"},
			absent:         []string{"/"},
		},
		{
			name: "pan-domain-enabled",
			subscribe: appconfig.SubscribeConfig{
				PanDomain: true,
			},
			wantRouteCount: 260,
			present:        []string{"/v1/subscribe/config", "/"},
		},
	}
	for _, tc := range routeCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			svcCtx := &svc.ServiceContext{Config: appconfig.Config{Subscribe: tc.subscribe}}
			router := server.Default()
			RegisterHandlers(router, svcCtx)
			routes := router.Routes()
			paths := make(map[string]struct{}, len(routes))
			for _, route := range routes {
				paths[route.Path] = struct{}{}
			}

			// When
			for _, path := range tc.present {
				_, registered := paths[path]
				if !registered {
					t.Fatalf("expected route %q to be registered", path)
				}
			}
			for _, path := range tc.absent {
				_, registered := paths[path]
				if registered {
					t.Fatalf("expected route %q to be absent", path)
				}
			}

			// Then
			if len(routes) != tc.wantRouteCount {
				t.Fatalf("expected %d routes, got %d", tc.wantRouteCount, len(routes))
			}
		})
	}

	requestCases := []struct {
		name      string
		path      string
		subscribe appconfig.SubscribeConfig
		host      string
	}{
		{
			name: "fallback-access-denied",
			path: "/v1/subscribe/config?token=route-contract-token",
			subscribe: appconfig.SubscribeConfig{
				PanDomain: true,
			},
			host: "mismatch",
		},
		{
			name: "custom-access-denied",
			path: "/custom/subscribe?token=route-contract-token",
			subscribe: appconfig.SubscribeConfig{
				SubscribePath: "/custom/subscribe",
				PanDomain:     true,
			},
			host: "mismatch",
		},
		{
			name: "root-access-denied",
			path: "/",
			subscribe: appconfig.SubscribeConfig{
				PanDomain: true,
			},
			host: "localhost",
		},
	}
	for _, tc := range requestCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			router := server.Default()
			RegisterHandlers(router, &svc.ServiceContext{Config: appconfig.Config{Subscribe: tc.subscribe}})
			ctx := router.NewContext()
			ctx.Request.SetRequestURI(tc.path)
			ctx.Request.Header.SetMethod(http.MethodGet)
			ctx.Request.SetHost(tc.host)

			// When
			router.ServeHTTP(context.Background(), ctx)

			// Then
			if ctx.Response.StatusCode() != http.StatusForbidden || string(ctx.Response.Body()) != "Access denied" {
				t.Fatalf("expected access-denied response before datastore access, got (%d, %q)", ctx.Response.StatusCode(), ctx.Response.Body())
			}
		})
	}
}

func TestRegisterHandlers_middlewareContracts(t *testing.T) {
	tests := []struct {
		name     string
		config   appconfig.Config
		paths    []string
		wantCode uint32
		wantMsg  string
	}{
		{
			name:     "admin-auth",
			paths:    []string{"/v1/admin/plugin/list", "/v1/admin/plugins"},
			wantCode: xerr.ErrorTokenEmpty,
			wantMsg:  "User token is empty",
		},
		{
			name: "public-auth-before-device",
			config: appconfig.Config{Device: appconfig.DeviceConfig{
				Enable: true,
			}},
			paths:    []string{"/v1/public/announcement/list"},
			wantCode: xerr.ErrorTokenEmpty,
			wantMsg:  "User token is empty",
		},
		{
			name: "device-only",
			config: appconfig.Config{Device: appconfig.DeviceConfig{
				Enable: true,
			}},
			paths:    []string{"/v1/auth/check"},
			wantCode: xerr.SecretIsEmpty,
			wantMsg:  "Secret is empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			router := server.Default()
			RegisterHandlers(router, &svc.ServiceContext{Config: tc.config})

			for _, path := range tc.paths {
				ctx := router.NewContext()
				ctx.Request.SetRequestURI(path)
				ctx.Request.Header.SetMethod(http.MethodGet)

				// When
				router.ServeHTTP(context.Background(), ctx)

				// Then
				var response struct {
					Code uint32 `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
					t.Fatalf("unmarshal %s envelope: %v", path, err)
				}
				if response.Code != tc.wantCode || response.Msg != tc.wantMsg {
					t.Fatalf("expected %s envelope (%d, %q), got (%d, %q)", path, tc.wantCode, tc.wantMsg, response.Code, response.Msg)
				}
			}
		})
	}
}
