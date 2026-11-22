package setup

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server/render"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/logger/logtest"
	"github.com/perfect-panel/server/pkg/orm"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type failingCloser struct{ err error }

func (c failingCloser) Close() error { return c.err }

// The setup handlers used to drop the error of closing their connection pool.
func TestCloseDatabaseLogsFailures(t *testing.T) {
	logs := logtest.NewCollector(t)

	closeDatabase(failingCloser{err: errors.New("connection reset")})

	if out := logs.String(); !strings.Contains(out, "close database connection failed") || !strings.Contains(out, "connection reset") {
		t.Fatalf("log = %s, want the close failure", out)
	}

	logs.Reset()
	closeDatabase(failingCloser{})
	if out := logs.String(); out != "" {
		t.Fatalf("a clean close logged %s", out)
	}
}

func TestNewConfigServer_rendersInitAndRedirectsUnknownRoutes(t *testing.T) {
	// Given
	engine := newConfigServer(newWizard(filepath.Join(t.TempDir(), "ppanel.yaml")))
	templates := template.Must(template.ParseFS(templateFS, "templates/*.html"))

	initRequest := engine.NewContext()
	initRequest.HTMLRender = render.HTMLProduction{Template: templates}
	initRequest.Request.SetRequestURI("/init")
	initRequest.Request.Header.SetMethod(http.MethodGet)

	unknownRequest := engine.NewContext()
	unknownRequest.Request.SetRequestURI("/unknown")
	unknownRequest.Request.Header.SetMethod(http.MethodGet)

	// When
	engine.ServeHTTP(context.Background(), initRequest)
	engine.ServeHTTP(context.Background(), unknownRequest)

	// Then
	if status := initRequest.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("expected init status %d, got %d", http.StatusOK, status)
	}
	if len(initRequest.Response.Body()) == 0 {
		t.Fatal("expected init HTML response body")
	}
	if status := unknownRequest.Response.StatusCode(); status != http.StatusFound {
		t.Fatalf("expected redirect status %d, got %d", http.StatusFound, status)
	}
	if location := string(unknownRequest.Response.Header.Peek("Location")); location != "/init" {
		t.Fatalf("expected redirect location %q, got %q", "/init", location)
	}
}

// A new database stores times in the application's zone, the configuration
// file's AppLocation; the parameters are written out so a later AppLocation
// change cannot reinterpret them.
func TestBuildDatabaseConfigUsesTheAppLocation(t *testing.T) {
	for driver, want := range map[string]string{
		"mysql":    "loc=Europe%2FParis",
		"postgres": "TimeZone=Europe/Paris",
	} {
		cfg, err := buildDatabaseConfig(driver, "127.0.0.1", "3306", "ppanel", "root", "secret", "Europe/Paris")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(cfg.Config, want) {
			t.Errorf("%s parameters = %q, want %s", driver, cfg.Config, want)
		}
	}
}

// The page's own checks are not trusted: the handler refuses a short
// password and an email that is no address before anything is touched.
func TestInitRequestValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		request initRequest
		want    string
	}{
		"valid":            {initRequest{AdminEmail: "admin@example.com", AdminPassword: "12345678"}, ""},
		"seven characters": {initRequest{AdminEmail: "admin@example.com", AdminPassword: "1234567"}, "at least 8 characters"},
		"empty password":   {initRequest{AdminEmail: "admin@example.com"}, "at least 8 characters"},
		"no email":         {initRequest{AdminPassword: "12345678"}, "email"},
		"not an email":     {initRequest{AdminEmail: "admin", AdminPassword: "12345678"}, "email"},
	} {
		email, got := tc.request.validate()
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: validate() = %q, want %q", name, got, tc.want)
		}
		if tc.want == "" && email != "admin@example.com" {
			t.Errorf("%s: validate() email = %q, want the canonical admin@example.com", name, email)
		}
	}
}

// installation is a wizard on a temporary configuration file whose database
// steps are fakes: connect opens an in-memory SQLite database, migrate and
// createAdmin record their calls.
type installation struct {
	*wizard
	configPath string
	db         *gorm.DB
	mu         sync.Mutex
	migrations int
	admins     []string
	migrateErr error
}

func newInstallation(t *testing.T) *installation {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	path := filepath.Join(t.TempDir(), "ppanel.yaml")
	if err := os.WriteFile(path, []byte("AppLocation: Europe/Paris\nPort: 9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &installation{wizard: newWizard(path), configPath: path, db: db}
	inst.connect = func(orm.Config) (*gorm.DB, error) {
		// The handler closes what it opened; hand it a session on the shared
		// database rather than the pool the test keeps.
		return db.Session(&gorm.Session{}), nil
	}
	inst.migrate = func(string, string) error {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		inst.migrations++
		return inst.migrateErr
	}
	inst.createAdmin = func(email, _ string, _ *gorm.DB) error {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		inst.admins = append(inst.admins, email)
		return nil
	}
	return inst
}

type initResponse struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	Status bool   `json:"status"`
}

// submit posts the installer's form and returns the HTTP status and body.
func (inst *installation) submit(t *testing.T, body string) (int, initResponse) {
	t.Helper()
	engine := newConfigServer(inst.wizard)
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/init/config")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.Header.SetContentTypeBytes([]byte("application/json"))
	ctx.Request.SetBody([]byte(body))
	engine.ServeHTTP(context.Background(), ctx)
	var response initResponse
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("response %q: %v", ctx.Response.Body(), err)
	}
	return ctx.Response.StatusCode(), response
}

const validForm = `{"adminEmail":"Admin@Example.com","adminPassword":"correct horse","databaseDriver":"mysql","mysqlHost":"127.0.0.1","mysqlPort":"3306","mysqlDatabase":"ppanel","mysqlUser":"root","mysqlPassword":"secret","redisHost":"127.0.0.1","redisPort":"6379","redisPassword":"r","redisUsername":"panel","redisKeyPrefix":"prod"}`

func (inst *installation) file(t *testing.T) config.File {
	t.Helper()
	data, err := os.ReadFile(inst.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var written config.File
	if err := yaml.Unmarshal(data, &written); err != nil {
		t.Fatalf("written file: %v\n%s", err, data)
	}
	return written
}

// A complete installation writes the file, migrates, creates the
// administrator, signals the caller once, and refuses a second submission.
func TestHandleInitConfigInstalls(t *testing.T) {
	logtest.Discard(t)
	inst := newInstallation(t)

	status, response := inst.submit(t, validForm)

	if status != http.StatusOK || !response.Status {
		t.Fatalf("submit = %d %+v, want 200 installed", status, response)
	}
	written := inst.file(t)
	if written.Database.Addr != "127.0.0.1:3306" || written.Database.Password != "secret" || written.Redis.Host != "127.0.0.1:6379" || written.Redis.Username != "panel" || written.Redis.KeyPrefix != "prod" || written.JwtAuth.AccessSecret == "" {
		t.Fatalf("written file %+v, want the connections and a secret", written)
	}
	if written.Administrator.Email != "admin@example.com" || written.Administrator.Password != "" {
		t.Fatalf("Administrator = %+v, want the installer's canonical email and no password", written.Administrator)
	}
	if !strings.Contains(written.Database.Config, "loc=Europe%2FParis") || written.AppLocation != "Europe/Paris" || written.Port != 9090 {
		t.Fatalf("written file %+v, want the file's AppLocation and Port kept", written)
	}
	if info, err := os.Stat(inst.configPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v (%v), want 0600", info.Mode(), err)
	}
	if inst.migrations != 1 || len(inst.admins) != 1 || inst.admins[0] != "admin@example.com" {
		t.Fatalf("migrations %d, admins %v; want one each, the administrator canonical", inst.migrations, inst.admins)
	}
	select {
	case err := <-inst.done:
		if err != nil {
			t.Fatalf("done = %v, want nil", err)
		}
	default:
		t.Fatal("the caller was not signalled")
	}

	status, response = inst.submit(t, validForm)
	if status != http.StatusConflict {
		t.Fatalf("second submit = %d %+v, want 409", status, response)
	}
	if inst.migrations != 1 {
		t.Fatal("the second submission migrated again")
	}
}

// Input the page would have refused is refused here too, before the file or
// the database is touched.
func TestHandleInitConfigRejectsBadInput(t *testing.T) {
	logtest.Discard(t)
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"short password": {strings.Replace(validForm, "correct horse", "1234567", 1), "8 characters"},
		"invalid email":  {strings.Replace(validForm, "Admin@Example.com", "admin", 1), "email"},
		"unknown driver": {strings.Replace(validForm, `"mysql"`, `"oracle"`, 1), "unsupported database driver"},
		"not json":       {"{", "Invalid request"},
	} {
		t.Run(name, func(t *testing.T) {
			inst := newInstallation(t)
			before, _ := os.ReadFile(inst.configPath)

			status, response := inst.submit(t, tc.body)

			if status != http.StatusBadRequest || !strings.Contains(response.Msg, tc.want) {
				t.Fatalf("submit = %d %+v, want 400 mentioning %q", status, response, tc.want)
			}
			if after, _ := os.ReadFile(inst.configPath); string(after) != string(before) {
				t.Fatal("the configuration file was written")
			}
			if inst.migrations != 0 || len(inst.admins) != 0 {
				t.Fatal("the database was touched")
			}
			if !inst.begin() {
				t.Fatal("the wizard stayed claimed after a rejected submission")
			}
		})
	}
}

// A database that already holds tables is refused here, not only by the
// connection test the page runs, which a direct POST skips.
func TestHandleInitConfigRefusesADatabaseWithData(t *testing.T) {
	logtest.Discard(t)
	inst := newInstallation(t)
	if err := inst.db.Exec("CREATE TABLE user (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(inst.configPath)

	status, response := inst.submit(t, validForm)

	if status != http.StatusBadRequest || !strings.Contains(response.Msg, "existing data") {
		t.Fatalf("submit = %d %+v, want 400 existing data", status, response)
	}
	if after, _ := os.ReadFile(inst.configPath); string(after) != string(before) {
		t.Fatal("the configuration file was written for a database with data")
	}
	if inst.migrations != 0 {
		t.Fatal("a database with data was migrated")
	}
}

// The configuration file is written before the migration, so a migration
// that fails leaves an installation the next start resumes; the wizard
// reports that and does not signal completion.
func TestHandleInitConfigWritesTheFileBeforeMigrating(t *testing.T) {
	logtest.Discard(t)
	inst := newInstallation(t)
	inst.migrateErr = errors.New("dirty database version 3")

	status, response := inst.submit(t, validForm)

	if status != http.StatusInternalServerError || !strings.Contains(response.Msg, "configuration was saved") {
		t.Fatalf("submit = %d %+v, want 500 with the resumable installation explained", status, response)
	}
	written := inst.file(t)
	if written.Database.Addr != "127.0.0.1:3306" || written.Administrator.Email != "admin@example.com" {
		t.Fatalf("written file %+v, want the connections and administrator saved before the migration", written)
	}
	if len(inst.admins) != 0 {
		t.Fatal("the administrator was created after a failed migration")
	}
	select {
	case err := <-inst.done:
		t.Fatalf("the caller was signalled (%v) after a failed installation", err)
	default:
	}
	if !inst.begin() {
		t.Fatal("the wizard stayed claimed after a failed installation")
	}
}

// Two submissions at once: one installs, the other is refused, and the
// caller is signalled once.
func TestHandleInitConfigRefusesConcurrentSubmissions(t *testing.T) {
	logtest.Discard(t)
	inst := newInstallation(t)
	release := make(chan struct{})
	inst.migrate = func(string, string) error {
		<-release
		return nil
	}

	first := make(chan int, 1)
	go func() {
		status, _ := inst.submit(t, validForm)
		first <- status
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		inst.wizard.mu.Lock()
		installing := inst.wizard.installing
		inst.wizard.mu.Unlock()
		if installing || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	status, response := inst.submit(t, validForm)
	if status != http.StatusConflict || !strings.Contains(response.Msg, "in progress") {
		t.Fatalf("concurrent submit = %d %+v, want 409 in progress", status, response)
	}
	close(release)
	if got := <-first; got != http.StatusOK {
		t.Fatalf("first submit = %d, want 200", got)
	}
	if len(inst.admins) != 1 {
		t.Fatalf("admins = %v, want exactly one", inst.admins)
	}
	if err := <-inst.done; err != nil {
		t.Fatalf("installation signalled a failure: %v", err)
	}
	select {
	case err := <-inst.done:
		t.Fatalf("the caller was signalled twice: %v", err)
	default:
	}
}

// The wizard listens on the loopback interface and the configured port, and
// a port it cannot bind is reported to the caller instead of ending the
// process from a goroutine.
func TestStartHonoursThePortAndReportsAListenerItCannotBind(t *testing.T) {
	logtest.Discard(t)
	if Address(0) != "127.0.0.1:8080" || Address(9090) != "127.0.0.1:9090" {
		t.Fatalf("Address = %s, %s; want the loopback interface with the default and the configured port", Address(0), Address(9090))
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	path := filepath.Join(t.TempDir(), "ppanel.yaml")

	status, engine := Start(path, port)
	t.Cleanup(func() { _ = engine.Shutdown(context.Background()) })
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if response, err = client.Get("http://" + Address(port) + "/init"); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the wizard does not answer on %s: %v", Address(port), err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /init = %d, want 200", response.StatusCode)
	}
	select {
	case err := <-status:
		t.Fatalf("status = %v before any installation", err)
	default:
	}

	// The same port again: the second wizard cannot bind and says so.
	failed, second := Start(path, port)
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })
	select {
	case err := <-failed:
		if err == nil || !strings.Contains(err.Error(), "listen on "+Address(port)) {
			t.Fatalf("status = %v, want the bind failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the listener that cannot bind was not reported")
	}
}
