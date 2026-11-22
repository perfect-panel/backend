package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/conf"
	"gopkg.in/yaml.v3"
)

// The installation rewrites the configuration file from File; the
// application zone must survive the rewrite.
func TestFileKeepsTheAppLocation(t *testing.T) {
	data, err := yaml.Marshal(File{AppLocation: "Europe/Paris"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "AppLocation: Europe/Paris") {
		t.Fatalf("file = %s, want the AppLocation", data)
	}
	var back File
	if err := yaml.Unmarshal(data, &back); err != nil || back.AppLocation != "Europe/Paris" {
		t.Fatalf("read back %q (%v), want Europe/Paris", back.AppLocation, err)
	}
}

// The two halves of Config share one level in the file, and both get their
// defaults.
func TestConfigLoadsBothHalvesFromOneLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ppanel.yaml")
	content := "Host: 127.0.0.1\nSite:\n  SiteName: Panel\nTelegram:\n  Enable: true\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var c Config
	if err := conf.Load(path, &c); err != nil {
		t.Fatal(err)
	}
	if c.Host != "127.0.0.1" || c.Site.SiteName != "Panel" || !c.Telegram.Enable {
		t.Fatalf("loaded %+v, want the file's boot and runtime settings", c)
	}
	if c.Port != 8080 || c.AppLocation != "Asia/Shanghai" {
		t.Fatalf("defaults = port %d, zone %q; want 8080 and Asia/Shanghai", c.Port, c.AppLocation)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Boot:") || strings.Contains(string(data), "Runtime:") || !strings.Contains(string(data), "\nSite:") {
		t.Fatalf("written file nests the halves:\n%s", data)
	}
}
