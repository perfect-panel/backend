package tool

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/internal/app/buildinfo"
	"github.com/perfect-panel/backend/internal/infra/geoip/geoiptest"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/platform/transport/http/internal/handlertest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The tool handlers run against the real platform facade: the admin tools
// need no repository, only the log directory, the GeoIP database and the
// restart hook, which the tests provide.

// toolRoutes registers the tool handlers on their production routes.
func toolRoutes(svc platform.Service) *server.Hertz {
	h := server.New()
	group := h.Group("/v1/admin/tool")
	group.GET("/ip/location", QueryIPLocationHandler(svc))
	group.GET("/log", GetSystemLogHandler(svc))
	group.GET("/restart", RestartSystemHandler(svc))
	group.GET("/version", GetVersionHandler(svc))
	return h
}

// logDir writes the log files into a fresh log directory.
func logDir(t *testing.T, files map[string][]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, lines := range files {
		content := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// entry is one JSON log line as the logger writes it, at minute of the
// hour.
func entry(minute int, content string) string {
	return fmt.Sprintf(`{"caller":"http/server.go:42","content":%q,"level":"info","timestamp":"2026-09-28T10:%02d:00.000+08:00"}`, content, minute)
}

func serveLog(t *testing.T, dir string) handlertest.Reply {
	t.Helper()
	return handlertest.Serve(t, toolRoutes(platform.New(platform.Deps{LogPath: dir})), http.MethodGet, "/v1/admin/tool/log", "")
}

// The tail merges the log files, skips the lines that are not JSON entries
// and shows the entries newest first, as the admin panel lists them.
func TestSystemLogTailShowsTheNewestEntriesFirst(t *testing.T) {
	logtest.Discard(t)
	dir := logDir(t, map[string][]string{
		"access.log": {entry(1, "started"), "panic: not a JSON line", entry(3, "served")},
		"error.log":  {entry(2, "slow query")},
	})

	serveLog(t, dir).OK(t, `{"list":[`+entry(3, "served")+`,`+entry(2, "slow query")+`,`+entry(1, "started")+`]}`)
}

// The tail reads the latest fifty entries of each file and shows the fifty
// newest of them.
func TestSystemLogTailKeepsTheFiftyNewestEntries(t *testing.T) {
	logtest.Discard(t)
	var access []string
	for minute := 0; minute < 55; minute++ {
		access = append(access, entry(minute, "request"))
	}
	dir := logDir(t, map[string][]string{
		"access.log": access,
		"error.log":  {entry(57, "error 1"), entry(58, "error 2"), entry(59, "error 3")},
	})

	var tail struct {
		List []map[string]any `json:"list"`
	}
	serveLog(t, dir).Decode(t, &tail)
	if len(tail.List) != 50 {
		t.Fatalf("tail has %d entries, want 50", len(tail.List))
	}
	first, last := tail.List[0]["timestamp"], tail.List[49]["timestamp"]
	if first != "2026-09-28T10:59:00.000+08:00" || last != "2026-09-28T10:08:00.000+08:00" {
		t.Fatalf("tail runs from %v to %v, want the error at 10:59 down to the request at 10:08", first, last)
	}
}

// Log files that exist but are empty, as right after a rotation, give an
// empty list rather than null.
func TestSystemLogTailOfEmptyFilesIsAnEmptyList(t *testing.T) {
	logtest.Discard(t)
	dir := t.TempDir()
	for _, name := range []string{"access.log", "error.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	serveLog(t, dir).OK(t, `{"list":[]}`)
}

// A tail that cannot be read, or shows no entry at all, is an error rather
// than an empty list: the admin panel would otherwise suggest a quiet server.
func TestSystemLogTailFailsWithoutReadableEntries(t *testing.T) {
	logtest.Discard(t)
	for name, dir := range map[string]string{
		"missing directory": filepath.Join(t.TempDir(), "absent"),
		"no log files":      t.TempDir(),
		"no JSON entries":   logDir(t, map[string][]string{"access.log": {"plain text", "more text"}}),
	} {
		t.Run(name, func(t *testing.T) {
			serveLog(t, dir).Refused(t, xerr.ERROR, "Internal Server Error")
		})
	}
}

// The restart answers at once and restarts the server in the background:
// the restart replaces the server answering the request.
func TestRestartSystemAnswersBeforeTheRestart(t *testing.T) {
	logtest.Discard(t)
	for name, outcome := range map[string]error{"restarted": nil, "restart failed": errors.New("listener busy")} {
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			restarted := make(chan struct{})
			svc := platform.New(platform.Deps{Restart: func() error {
				<-release
				close(restarted)
				return outcome
			}})

			// The restart is still waiting for release: the reply cannot
			// have waited for it. Its outcome is only logged.
			handlertest.Serve(t, toolRoutes(svc), http.MethodGet, "/v1/admin/tool/restart", "").OK(t, nil)
			close(release)
			select {
			case <-restarted:
			case <-time.After(5 * time.Second):
				t.Fatal("the server was never restarted")
			}
		})
	}
}

func TestVersionShowsTheBuild(t *testing.T) {
	reply := handlertest.Serve(t, toolRoutes(platform.New(platform.Deps{})), http.MethodGet, "/v1/admin/tool/version", "")
	reply.OK(t, map[string]string{"version": buildinfo.Display()})
}

// cityDatabase is a GeoLite2 City database locating the addresses below
// 128.0.0.0 in Sydney and knowing nothing of the others.
func cityDatabase(t *testing.T) func() *geoip2.Reader {
	t.Helper()
	reader, err := geoip2.FromBytes(geoiptest.MMDB("GeoLite2-City", map[string]any{
		"country": map[string]any{"iso_code": "AU", "names": map[string]any{"en": "Australia", "ja": "オーストラリア"}},
		"city":    map[string]any{"names": map[string]any{"en": "Sydney"}},
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return func() *geoip2.Reader { return reader }
}

func TestIPLocationAnswersTheEnglishNames(t *testing.T) {
	logtest.Discard(t)
	svc := platform.New(platform.Deps{GeoIP: cityDatabase(t)})
	for _, tc := range []struct{ ip, want string }{
		// The database has no subdivision for the address: the region is
		// left out.
		{"1.1.1.1", `{"country":"Australia","city":"Sydney"}`},
		// An address the database does not cover has no names.
		{"203.0.113.7", `{"country":"","city":""}`},
	} {
		t.Run(tc.ip, func(t *testing.T) {
			handlertest.Serve(t, toolRoutes(svc), http.MethodGet, "/v1/admin/tool/ip/location?ip="+tc.ip, "").OK(t, tc.want)
		})
	}
}

func TestIPLocationRefusals(t *testing.T) {
	logtest.Discard(t)
	located := cityDatabase(t)
	for _, tc := range []struct {
		name         string
		geoIP        func() *geoip2.Reader
		target, body string
		code         uint32
		msg          string
	}{
		{"without an address", located, "/v1/admin/tool/ip/location", "", xerr.InvalidParams, "IP is a required field"},
		// Some clients send a JSON document even with a GET.
		{"body not JSON", located, "/v1/admin/tool/ip/location", `{"ip":`, xerr.InvalidParams, ""},
		{"not an address", located, "/v1/admin/tool/ip/location?ip=999.1.1.1", "", xerr.InvalidParams, "Param Error"},
		{"IPv6 address in an IPv4 database", located, "/v1/admin/tool/ip/location?ip=2001:db8::1", "", xerr.ERROR, "Internal Server Error"},
		{"database not configured", nil, "/v1/admin/tool/ip/location?ip=1.1.1.1", "", xerr.ERROR, "Internal Server Error"},
		{"database not loaded yet", func() *geoip2.Reader { return nil }, "/v1/admin/tool/ip/location?ip=1.1.1.1", "", xerr.ERROR, "Internal Server Error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := platform.New(platform.Deps{GeoIP: tc.geoIP})
			handlertest.Serve(t, toolRoutes(svc), http.MethodGet, tc.target, tc.body).Refused(t, tc.code, tc.msg)
		})
	}
}
