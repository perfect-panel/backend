package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/orm"
	"gopkg.in/yaml.v3"
)

// The environment-variable installation completes the default configuration
// file with the connections PPANEL_DB and PPANEL_REDIS name and writes it
// back. The database session zone follows the file's AppLocation, and every
// boot setting the file already had survives the rewrite.
func TestInitConfigInstallsFromTheEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("etc", 0o755); err != nil {
		t.Fatal(err)
	}
	content := "AppLocation: Europe/Paris\n" +
		"Transport:\n  Driver: hertz\n" +
		"TLS:\n  Enable: true\n  CertFile: /etc/ppanel/tls.crt\n  KeyFile: /etc/ppanel/tls.key\n" +
		"EdgeSubscribe:\n  Enabled: true\n  MaxClockSkewSeconds: 60\n  Keys:\n    - ID: worker\n      Secret: worker-secret\n"
	if err := os.WriteFile(filepath.Join("etc", "ppanel.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PPANEL_DB", "root:db-secret@tcp(127.0.0.1:3306)/ppanel")
	t.Setenv("PPANEL_REDIS", "redis://panel:redis-secret@127.0.0.1:6379/2?prefix=prod")

	var c config.Config
	if initConfig(&c) {
		t.Fatal("initConfig() asks for the wizard although the environment completes the configuration")
	}

	data, err := os.ReadFile(filepath.Join("etc", "ppanel.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var written config.File
	if err := yaml.Unmarshal(data, &written); err != nil {
		t.Fatalf("written file: %v\n%s", err, data)
	}
	if written.AppLocation != "Europe/Paris" {
		t.Fatalf("AppLocation = %q, want Europe/Paris", written.AppLocation)
	}
	if zone := (orm.Mysql{Config: written.Database}).SessionLocation(); zone != "Europe/Paris" {
		t.Fatalf("database session zone = %q, want the AppLocation (parameters %q)", zone, written.Database.Config)
	}
	if written.Database.Addr != "127.0.0.1:3306" || written.Database.Dbname != "ppanel" || written.Database.Username != "root" || written.Database.Password != "db-secret" {
		t.Fatalf("database = %+v, want the PPANEL_DB connection", written.Database)
	}
	if written.Redis.Host != "127.0.0.1:6379" || written.Redis.Pass != "redis-secret" || written.Redis.DB != 2 || written.Redis.Username != "panel" || written.Redis.KeyPrefix != "prod" {
		t.Fatalf("redis = %+v, want the PPANEL_REDIS connection", written.Redis)
	}
	if written.JwtAuth.AccessSecret == "" {
		t.Fatal("the written file has no JWT secret")
	}
	if written.Transport.Driver != "hertz" {
		t.Fatalf("Transport = %+v, want the file's transport kept", written.Transport)
	}
	if !written.TLS.Enable || written.TLS.CertFile != "/etc/ppanel/tls.crt" || written.TLS.KeyFile != "/etc/ppanel/tls.key" {
		t.Fatalf("TLS = %+v, want the file's TLS settings kept", written.TLS)
	}
	if !written.EdgeSubscribe.Enabled || written.EdgeSubscribe.MaxClockSkewSeconds != 60 || len(written.EdgeSubscribe.Keys) != 1 || written.EdgeSubscribe.Keys[0].ID != "worker" {
		t.Fatalf("EdgeSubscribe = %+v, want the file's edge subscribe settings kept", written.EdgeSubscribe)
	}
	// The process starts on the configuration it wrote.
	if c.DatabaseConfig() != written.Database || c.Redis != written.Redis || c.JwtAuth != written.JwtAuth {
		t.Fatalf("loaded configuration %+v differs from the written file", c.Boot)
	}
	if written.Administrator.Email != "admin@ppanel.dev" {
		t.Fatalf("Administrator = %+v, want the default email written for the seed", written.Administrator)
	}
}

// useConfigPath points the run command at path until the test ends.
func useConfigPath(t *testing.T, path string) {
	t.Helper()
	previous := startConfigPath
	startConfigPath = path
	t.Cleanup(func() { startConfigPath = previous })
}

// The environment-variable installation follows the file's content, not its
// path: an empty file at any --config path is completed from PPANEL_DB and
// PPANEL_REDIS. Only the default etc/ppanel.yaml used to be, so the
// documented systemd unit with an absolute --config silently ran the wizard.
func TestInitConfigInstallsFromTheEnvironmentAtACustomPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opt", "ppanel-server", "etc", "ppanel.yaml")
	useConfigPath(t, path)
	createConfigFileIfMissing()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("created file: %v, mode %v; want an empty 0600 file in a created directory", err, info.Mode())
	}
	t.Setenv("PPANEL_DB", "postgres://ppanel:db-secret@127.0.0.1:5432/ppanel")
	t.Setenv("PPANEL_REDIS", "redis://:redis-secret@127.0.0.1:6379/1")

	var c config.Config
	if initConfig(&c) {
		t.Fatal("initConfig() asks for the wizard at a custom path although the environment completes the configuration")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var written config.File
	if err := yaml.Unmarshal(data, &written); err != nil {
		t.Fatalf("written file: %v\n%s", err, data)
	}
	if written.Database.Driver != orm.DriverPostgres || written.Database.Addr != "127.0.0.1:5432" || written.Database.Password != "db-secret" {
		t.Fatalf("database = %+v, want the PPANEL_DB connection", written.Database)
	}
	if written.Redis.Host != "127.0.0.1:6379" || written.Redis.DB != 1 || written.JwtAuth.AccessSecret == "" {
		t.Fatalf("redis = %+v, secret %q; want the PPANEL_REDIS connection and a generated secret", written.Redis, written.JwtAuth.AccessSecret)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("written file mode %v (%v), want 0600", info.Mode(), err)
	}
	// A second start finds the file complete and neither rewrites it nor
	// asks for the wizard.
	var again config.Config
	if initConfig(&again) || again.JwtAuth.AccessSecret != written.JwtAuth.AccessSecret {
		t.Fatal("a complete file was not accepted as is")
	}
}

// Without the environment variables an incomplete file means the wizard,
// whatever its path and whether it already holds a secret; a file that names
// a database is complete and is left to getServers' secret check.
func TestInitConfigStartsTheWizardForAnIncompleteFile(t *testing.T) {
	for name, content := range map[string]string{
		"empty file":             "",
		"secret but no database": "JwtAuth:\n  AccessSecret: already-set\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "custom.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			useConfigPath(t, path)
			t.Setenv("PPANEL_DB", "")
			t.Setenv("PPANEL_REDIS", "")

			var c config.Config
			if !initConfig(&c) {
				t.Fatal("initConfig() did not ask for the wizard on an incomplete file")
			}
			if data, _ := os.ReadFile(path); string(data) != content {
				t.Fatalf("the file was rewritten to %q", data)
			}
		})
	}

	// PPANEL_DB set but PPANEL_REDIS missing or malformed: the wizard, and
	// the file untouched.
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	useConfigPath(t, path)
	t.Setenv("PPANEL_DB", "root:pw@tcp(127.0.0.1:3306)/ppanel")
	t.Setenv("PPANEL_REDIS", "")
	// Each start loads into a fresh configuration, as the command does.
	if !initConfig(new(config.Config)) {
		t.Fatal("initConfig() completed the configuration without PPANEL_REDIS")
	}
	t.Setenv("PPANEL_REDIS", "redis://127.0.0.1:6379/not-a-number")
	if !initConfig(new(config.Config)) {
		t.Fatal("initConfig() completed the configuration with a malformed PPANEL_REDIS")
	}
	if data, _ := os.ReadFile(path); len(data) != 0 {
		t.Fatalf("the file was rewritten to %q", data)
	}

	// A file naming a database is complete, secret or not.
	if err := os.WriteFile(path, []byte("Database:\n  Addr: db:3306\n  Dbname: ppanel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if initConfig(new(config.Config)) {
		t.Fatal("initConfig() asks for the wizard although the file names a database")
	}
}
