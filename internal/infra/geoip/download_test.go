package geoip

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/internal/infra/geoip/geoiptest"
)

func TestDownloadGeoIPDatabaseInstallsVerifiedDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "GeoLite2-City.mmdb")
	server := serveBytes(t, buildTestMMDB(GeoIPDBType, map[string]any{"country": map[string]any{"iso_code": "AU"}}))

	if err := DownloadGeoIPDatabase(server.URL, path, GeoIPDBType); err != nil {
		t.Fatalf("DownloadGeoIPDatabase: %v", err)
	}
	db, err := geoip2.Open(path)
	if err != nil {
		t.Fatalf("installed database does not open: %v", err)
	}
	defer func() { _ = db.Close() }()
	record, err := db.City(net.IPv4(1, 1, 1, 1))
	if err != nil || record.Country.IsoCode != "AU" {
		t.Fatalf("lookup = %+v (err %v), want the served record", record, err)
	}
	assertOnlyCityDatabase(t, dir)
}

// A download that is not a complete database of the expected type must
// never replace the active database, and must not leave its temp file.
func TestDownloadGeoIPDatabaseRejectsUnverifiedDownloads(t *testing.T) {
	city := buildTestMMDB(GeoIPDBType, map[string]any{})
	corruptTree := append([]byte(nil), city...)
	// Both records point far past the end of the file.
	copy(corruptTree, []byte{0, 0x03, 0xF9, 0, 0x03, 0xF9})
	for name, body := range map[string][]byte{
		"error page":         []byte("<html>rate limited</html>"),
		"empty":              {},
		"truncated":          city[:len(city)/2],
		"last byte missing":  city[:len(city)-1],
		"wrong database":     buildTestMMDB(GeoIPASNDBType, map[string]any{}),
		"unknown database":   buildTestMMDB("Example-Other", map[string]any{}),
		"corrupt tree":       corruptTree,
		"country for a city": buildTestMMDB("GeoLite2-Country", map[string]any{}),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "GeoLite2-City.mmdb")
			active := []byte("previous database")
			if err := os.WriteFile(path, active, 0o644); err != nil {
				t.Fatal(err)
			}

			err := DownloadGeoIPDatabase(serveBytes(t, body).URL, path, GeoIPDBType)

			if err == nil {
				t.Fatal("unverified download was accepted")
			}
			t.Logf("rejected: %v", err)
			if current, _ := os.ReadFile(path); !bytes.Equal(current, active) {
				t.Fatal("unverified download replaced the active database")
			}
			assertOnlyCityDatabase(t, dir)
		})
	}
}

func TestDownloadGeoIPDatabaseVerifiesASNEdition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "GeoLite2-ASN.mmdb")
	asn := buildTestMMDB(GeoIPASNDBType, map[string]any{"autonomous_system_number": uint32(13335)})

	if err := DownloadGeoIPDatabase(serveBytes(t, asn).URL, path, GeoIPASNDBType); err != nil {
		t.Fatalf("ASN download: %v", err)
	}
	if err := DownloadGeoIPDatabase(serveBytes(t, buildTestMMDB(GeoIPDBType, map[string]any{})).URL, path, GeoIPASNDBType); err == nil {
		t.Fatal("a City database was accepted as the ASN database")
	}
}

func serveBytes(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

// assertOnlyCityDatabase checks that dir holds the City database and nothing
// else: no temporary file of a download, no ASN database.
func assertOnlyCityDatabase(t *testing.T, dir string) {
	t.Helper()
	const name = "GeoLite2-City.mmdb"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only %s", names, name)
	}
}

// buildTestMMDB encodes a minimal IPv4 MaxMind DB that answers every
// address with record.
func buildTestMMDB(databaseType string, record map[string]any) []byte {
	return geoiptest.MMDB(databaseType, record, record)
}
