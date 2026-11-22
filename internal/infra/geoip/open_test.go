package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// mirror serves body and counts the requests; it stands in for the built-in
// mirror or an operator's own URL.
type mirror struct {
	*httptest.Server
	requests atomic.Int32
}

func newMirror(t *testing.T, body []byte) *mirror {
	t.Helper()
	m := &mirror{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m.requests.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(m.Close)
	return m
}

// useMirrors points the built-in mirror at local servers until the test ends.
func useMirrors(t *testing.T, city, asn string) {
	t.Helper()
	previousCity, previousASN := cityDownloadURL, asnDownloadURL
	cityDownloadURL, asnDownloadURL = city, asn
	t.Cleanup(func() { cityDownloadURL, asnDownloadURL = previousCity, previousASN })
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cityDatabase(isoCode string) []byte {
	return buildTestMMDB(GeoIPDBType, map[string]any{"country": map[string]any{"iso_code": isoCode}})
}

func closeLocation(t *testing.T, loc *IPLocation) {
	t.Helper()
	if loc == nil {
		return
	}
	if loc.DB != nil {
		_ = loc.DB.Close()
	}
	if loc.ASNDB != nil {
		_ = loc.ASNDB.Close()
	}
}

// Without downloads a missing database is an error, reported without a
// single request to the mirror: the server used to insist on downloading it
// and refused to start offline.
func TestOpenWithoutDownloadsReportsAMissingDatabase(t *testing.T) {
	city := newMirror(t, cityDatabase("AU"))
	useMirrors(t, city.URL, city.URL)
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")

	_, err := Open(Options{Path: path, Download: false})

	if err == nil || !strings.Contains(err.Error(), "downloads are off") {
		t.Fatalf("Open() = %v, want the missing database reported", err)
	}
	if city.requests.Load() != 0 {
		t.Fatal("the mirror was contacted although downloads are off")
	}
	if _, err := Open(Options{}); err == nil {
		t.Fatal("Open() without a path returned nil")
	}
}

// An existing database is used as is, and with a configured digest only
// when it matches.
func TestOpenVerifiesTheConfiguredDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "GeoLite2-City.mmdb")
	data := cityDatabase("AU")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loc, err := Open(Options{Path: path, SHA256: digestOf(data)})
	if err != nil {
		t.Fatalf("Open() with the matching digest = %v", err)
	}
	closeLocation(t, loc)
	loc, err = Open(Options{Path: path, SHA256: strings.ToUpper(digestOf(data))})
	if err != nil {
		t.Fatalf("Open() with the upper-case digest = %v", err)
	}
	closeLocation(t, loc)

	_, err = Open(Options{Path: path, SHA256: digestOf([]byte("another file"))})
	if err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("Open() with a different digest = %v, want the mismatch", err)
	}
	if _, err := Open(Options{Path: path, SHA256: "not-hex"}); err == nil {
		t.Fatal("Open() accepted a malformed digest")
	}
	if current, _ := os.ReadFile(path); string(current) != string(data) {
		t.Fatal("the database was altered")
	}
}

// A database whose digest differs is downloaded again when downloads are on,
// and the download must have the digest before it replaces the file.
func TestOpenReplacesADatabaseWithTheWrongDigestFromTheMirror(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "GeoLite2-City.mmdb")
	stale, fresh := cityDatabase("AU"), cityDatabase("DE")
	if err := os.WriteFile(path, stale, 0o644); err != nil {
		t.Fatal(err)
	}

	// The mirror serves a file with another digest than the configured one.
	wrong := newMirror(t, cityDatabase("FR"))
	_, err := Open(Options{Path: path, Download: true, DownloadURL: wrong.URL, SHA256: digestOf(fresh)})
	if err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("Open() = %v, want the download rejected on its digest", err)
	}
	if current, _ := os.ReadFile(path); string(current) != string(stale) {
		t.Fatal("a download with the wrong digest replaced the database")
	}
	assertOnlyCityDatabase(t, dir)

	// The mirror serves the configured file.
	right := newMirror(t, fresh)
	loc, err := Open(Options{Path: path, Download: true, DownloadURL: right.URL, SHA256: digestOf(fresh)})
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer closeLocation(t, loc)
	if current, _ := os.ReadFile(path); string(current) != string(fresh) {
		t.Fatal("the database was not replaced by the download with the configured digest")
	}
	if got := loc.Enrich(requestmeta.New("1.1.1.1", "ua")).IPCountryCode; got != "DE" {
		t.Fatalf("lookup after the download = %q, want DE", got)
	}
}

// The configured URL replaces the built-in mirror for the City database, and
// the ASN database is then not fetched from the mirror: the operator has
// left it.
func TestOpenDownloadsFromTheConfiguredURLOnly(t *testing.T) {
	builtin := newMirror(t, buildTestMMDB(GeoIPASNDBType, map[string]any{"autonomous_system_number": uint32(13335)}))
	useMirrors(t, builtin.URL, builtin.URL)
	own := newMirror(t, cityDatabase("AU"))
	dir := t.TempDir()
	path := filepath.Join(dir, "GeoLite2-City.mmdb")

	loc, err := Open(Options{Path: path, Download: true, DownloadURL: own.URL})
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer closeLocation(t, loc)

	if own.requests.Load() != 1 || builtin.requests.Load() != 0 {
		t.Fatalf("requests: own %d, built-in mirror %d; want one to the configured URL only", own.requests.Load(), builtin.requests.Load())
	}
	assertOnlyCityDatabase(t, dir)
	if loc.ASNDB != nil {
		t.Fatal("an ASN database was opened although none was placed")
	}
	if got := loc.Enrich(requestmeta.New("1.1.1.1", "ua")).IPCountryCode; got != "AU" {
		t.Fatalf("lookup = %q, want AU", got)
	}
}

// With the built-in mirror both databases are downloaded and the enrichment
// carries the location and the network.
func TestOpenDownloadsBothDatabasesFromTheBuiltInMirror(t *testing.T) {
	city := newMirror(t, cityDatabase("AU"))
	asn := newMirror(t, buildTestMMDB(GeoIPASNDBType, map[string]any{"autonomous_system_number": uint32(13335)}))
	useMirrors(t, city.URL, asn.URL)
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")

	loc, err := NewIPLocation(path)
	if err != nil {
		t.Fatalf("NewIPLocation() = %v", err)
	}
	defer closeLocation(t, loc)

	metadata := loc.Enrich(requestmeta.New("1.1.1.1", "ua"))
	if metadata.IPCountryCode != "AU" || metadata.IPASN != 13335 {
		t.Fatalf("enriched %+v, want the country and the network", metadata)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), ASNDatabaseName)); err != nil {
		t.Fatalf("ASN database not installed: %v", err)
	}
}

// Without a database the enricher is a no-op that still normalizes.
func TestNilIPLocationEnrichesNothing(t *testing.T) {
	var loc *IPLocation
	metadata := loc.Enrich(requestmeta.New("1.1.1.1", "ua"))
	if metadata.IPCountryCode != "" || metadata.IPASN != 0 || metadata.ClientIP != "1.1.1.1" {
		t.Fatalf("nil enricher produced %+v", metadata)
	}
}
