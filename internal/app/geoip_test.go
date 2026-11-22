package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/geoip/geoiptest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// The GeoIP database is best-effort metadata: without one the server starts
// with geolocation disabled and says why, unless the configuration requires
// it. It used to end the process when the download from the mirror failed.
func TestOpenGeoIPIsOptionalUnlessRequired(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")

	logs := logtest.NewCollector(t)
	location := openGeoIP(config.GeoIPConfig{Path: missing, Download: false})
	if location != nil {
		t.Fatal("openGeoIP returned a location for a missing database")
	}
	if out := logs.String(); !strings.Contains(out, "geolocation disabled") || !strings.Contains(out, "downloads are off") {
		t.Fatalf("log = %s, want geolocation disabled with the reason", out)
	}
	// The nil location enriches nothing and panics nowhere.
	if got := location.Enrich(requestmeta.New("1.1.1.1", "ua")); got.IPCountryCode != "" || got.ClientIP != "1.1.1.1" {
		t.Fatalf("nil location enriched %+v", got)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("a required database that is missing did not end the start")
			}
		}()
		openGeoIP(config.GeoIPConfig{Path: missing, Download: false, Required: true})
	}()

	present := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	database := geoiptest.MMDB("GeoLite2-City", map[string]any{"country": map[string]any{"iso_code": "AU"}}, nil)
	if err := os.WriteFile(present, database, 0o644); err != nil {
		t.Fatal(err)
	}
	location = openGeoIP(config.GeoIPConfig{Path: present, Download: false, Required: true})
	if location == nil {
		t.Fatal("openGeoIP returned nil for a present database")
	}
	defer func() { _ = location.DB.Close() }()
	if got := location.Enrich(requestmeta.New("1.1.1.1", "ua")).IPCountryCode; got != "AU" {
		t.Fatalf("lookup = %q, want AU", got)
	}
}
