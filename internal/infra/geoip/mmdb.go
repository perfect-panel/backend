// Package geoip reads the local MaxMind GeoLite2 databases and derives the
// country, region, city and network of a client IP for the request metadata
// the logs and audit records carry. The lookups are local, so no client
// address leaves the server. A missing database can be downloaded, with the
// download verified — and checked against a configured digest — before it
// replaces the active file; without a database the enricher is a no-op.
package geoip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

const (
	// GeoIPDBURL and GeoIPASNDBURL are the built-in mirror the City and ASN
	// databases are downloaded from when they are missing and downloads are
	// on. It is a third party's copy: Options.SHA256 pins the City database
	// to a known file, and Options.DownloadURL replaces the mirror.
	GeoIPDBURL    = "https://raw.githubusercontent.com/adysec/IP_database/main/geolite/GeoLite2-City.mmdb"
	GeoIPASNDBURL = "https://raw.githubusercontent.com/adysec/IP_database/main/geolite/GeoLite2-ASN.mmdb"

	// Database types the downloads must declare in their metadata. The
	// mirror publishes no checksums, so the file itself is what gets
	// verified before it may replace the active database.
	GeoIPDBType    = "GeoLite2-City"
	GeoIPASNDBType = "GeoLite2-ASN"

	// ASNDatabaseName is the file the ASN database is looked for under,
	// next to the City database.
	ASNDatabaseName = "GeoLite2-ASN.mmdb"
)

// cityDownloadURL and asnDownloadURL are the built-in mirror as Open uses
// it; tests point them at a local server.
var (
	cityDownloadURL = GeoIPDBURL
	asnDownloadURL  = GeoIPASNDBURL
)

// Options locates the databases Open reads.
type Options struct {
	// Path of the City database. The ASN database is looked for next to it
	// as GeoLite2-ASN.mmdb.
	Path string
	// Download fetches the City database when it is missing (or does not
	// have the configured digest), from DownloadURL or the built-in mirror.
	// The ASN database is fetched only from the built-in mirror, so only
	// while DownloadURL is empty; an operator with a mirror of their own
	// places it next to the City database.
	Download bool
	// DownloadURL replaces the built-in mirror for the City database.
	DownloadURL string
	// SHA256 is the hex digest the City database must have; empty skips the
	// check. A file that differs is downloaded again when Download is on and
	// rejected otherwise; a download that differs never replaces the file.
	SHA256 string
}

// IPLocation holds the open City database and, when it could be opened,
// the ASN database. The readers are shared by every request and stay open
// for the life of the process. A nil IPLocation enriches nothing.
type IPLocation struct {
	Path    string
	DB      *geoip2.Reader
	ASNPath string
	ASNDB   *geoip2.Reader
}

// NewIPLocation opens the City database at path and the ASN database next
// to it, downloading either from the built-in mirror when it is missing. It
// is Open with downloads on and no digest.
func NewIPLocation(path string) (*IPLocation, error) {
	return Open(Options{Path: path, Download: true})
}

// Open opens the City database the options locate, downloading and
// verifying it when the options allow, and the ASN database next to it when
// there is one. The City database is required for a location; without the
// ASN database the network organization is omitted.
func Open(opts Options) (*IPLocation, error) {
	if opts.Path == "" {
		return nil, errors.New("geoip: no database path configured")
	}
	digest, err := parseDigest(opts.SHA256)
	if err != nil {
		return nil, err
	}
	cityURL := opts.DownloadURL
	if cityURL == "" {
		cityURL = cityDownloadURL
	}
	if err := ensureDatabase(opts.Path, cityURL, GeoIPDBType, digest, opts.Download); err != nil {
		return nil, err
	}

	db, err := geoip2.Open(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("geoip: open %s: %w", opts.Path, err)
	}

	ipLoc := &IPLocation{Path: opts.Path, DB: db}
	asnPath := filepath.Join(filepath.Dir(opts.Path), ASNDatabaseName)
	ipLoc.ASNPath = asnPath
	if _, err := os.Stat(asnPath); os.IsNotExist(err) {
		if !opts.Download || opts.DownloadURL != "" {
			logger.Infof("[GeoIP] No ASN database at %s; network organization will be omitted", asnPath)
			return ipLoc, nil
		}
		logger.Infof("[GeoIP] ASN database not found, downloading from %s", asnDownloadURL)
		if err := downloadDatabase(asnDownloadURL, asnPath, GeoIPASNDBType, nil); err != nil {
			// ASN enrichment is optional. A transient download problem must not
			// turn logging metadata into an application startup dependency.
			logger.Errorf("[GeoIP] Failed to download ASN database; network organization will be omitted: %v", err)
			return ipLoc, nil
		}
		logger.Infof("[GeoIP] ASN database downloaded successfully")
	}
	if asnDB, err := geoip2.Open(asnPath); err != nil {
		logger.Errorf("[GeoIP] Failed to open ASN database; network organization will be omitted: %v", err)
	} else {
		ipLoc.ASNDB = asnDB
	}
	return ipLoc, nil
}

// parseDigest decodes a hex SHA-256 digest; empty means none.
func parseDigest(hexDigest string) ([]byte, error) {
	hexDigest = strings.TrimSpace(hexDigest)
	if hexDigest == "" {
		return nil, nil
	}
	digest, err := hex.DecodeString(hexDigest)
	if err != nil || len(digest) != sha256.Size {
		return nil, fmt.Errorf("geoip: SHA256 %q is not a hex SHA-256 digest", hexDigest)
	}
	return digest, nil
}

// ensureDatabase makes sure a database of databaseType is at path, with
// digest when one is configured: an existing file with the right digest (or
// no digest to meet) is kept; a missing file, or one whose digest differs,
// is downloaded from url when download is on and an error otherwise.
func ensureDatabase(path, url, databaseType string, digest []byte, download bool) error {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		if digest == nil {
			return nil
		}
		matches, err := fileHasDigest(path, digest)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
		if !download {
			return fmt.Errorf("geoip: %s does not have the configured SHA256 digest", path)
		}
		logger.Errorf("[GeoIP] %s does not have the configured SHA256 digest, downloading it again from %s", path, url)
	case os.IsNotExist(err):
		if !download {
			return fmt.Errorf("geoip: %s does not exist and downloads are off", path)
		}
		logger.Infof("[GeoIP] Database not found, downloading from %s", url)
	default:
		return fmt.Errorf("geoip: %s: %w", path, err)
	}
	if err := downloadDatabase(url, path, databaseType, digest); err != nil {
		return fmt.Errorf("geoip: download %s: %w", databaseType, err)
	}
	logger.Infof("[GeoIP] Database downloaded successfully")
	return nil
}

// fileHasDigest reports whether the file at path has the SHA-256 digest.
func fileHasDigest(path string, digest []byte) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	// The file is only read; closing it cannot lose data.
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return false, err
	}
	return bytes.Equal(hash.Sum(nil), digest), nil
}

// Enrich performs at most one City and one ASN lookup for a public client IP.
// MMDB lookup errors are treated as misses so request handling remains
// independent from optional risk metadata. A nil receiver, the server
// without a database, only normalizes the metadata.
func (ipLoc *IPLocation) Enrich(metadata requestmeta.Metadata) requestmeta.Metadata {
	ip := net.ParseIP(metadata.ClientIP)
	if ipLoc == nil || ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return requestmeta.Normalize(metadata)
	}
	if ipLoc.DB != nil {
		if record, err := ipLoc.DB.City(ip); err == nil && record != nil {
			metadata.IPCountryCode = record.Country.IsoCode
			metadata.IPCountry = preferredGeoName(record.Country.Names)
			if len(record.Subdivisions) > 0 {
				metadata.IPRegion = preferredGeoName(record.Subdivisions[0].Names)
			}
			metadata.IPCity = preferredGeoName(record.City.Names)
		}
	}
	if ipLoc.ASNDB != nil {
		if record, err := ipLoc.ASNDB.ASN(ip); err == nil && record != nil {
			metadata.IPASN = record.AutonomousSystemNumber
			metadata.IPASOrganization = record.AutonomousSystemOrganization
		}
	}
	return requestmeta.Normalize(metadata)
}

func preferredGeoName(names map[string]string) string {
	for _, language := range []string{"en", "zh-CN", "zh"} {
		if name := names[language]; name != "" {
			return name
		}
	}
	return ""
}

// DownloadGeoIPDatabase fetches a database to path. The file only replaces
// path once it has been verified as a complete database of databaseType.
func DownloadGeoIPDatabase(url, path, databaseType string) error {
	return downloadDatabase(url, path, databaseType, nil)
}

// downloadDatabase fetches a database to path. The file only replaces path
// once it has the SHA-256 digest, when one is given, and has been verified
// as a complete database of databaseType.
func downloadDatabase(url, path, databaseType string, digest []byte) error {
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		logger.Errorf("[GeoIP] Failed to create directory: %v", err.Error())
		return err
	}

	// Write into a sibling temporary file so a timeout or truncated response
	// never leaves a corrupt database that blocks the next startup retry.
	out, err := os.CreateTemp(filepath.Dir(path), ".geoip-*.tmp")
	if err != nil {
		return err
	}
	tempPath := out.Name()
	committed := false
	defer func() {
		_ = out.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	client := &http.Client{Timeout: 2 * time.Minute}
	// The client's timeout bounds the whole download; no caller carries a
	// request context down to this start-up path.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	// The body is only read; closing it cannot lose data.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download GeoIP database: HTTP %d", resp.StatusCode)
	}

	const maxGeoIPDatabaseSize = int64(256 << 20)
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, maxGeoIPDatabaseSize+1))
	if err != nil {
		return err
	}
	if written > maxGeoIPDatabaseSize {
		return fmt.Errorf("download GeoIP database: response exceeds %d bytes", maxGeoIPDatabaseSize)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if digest != nil && !bytes.Equal(hash.Sum(nil), digest) {
		return fmt.Errorf("downloaded GeoIP database has SHA256 %x, want the configured %x", hash.Sum(nil), digest)
	}
	if err := verifyGeoIPDatabase(tempPath, databaseType); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

// verifyGeoIPDatabase checks a downloaded file before it may replace the
// active database. It must open as a MaxMind DB — the metadata section sits
// at the very end of the file, so a truncated transfer fails here — declare
// the expected database type, and answer a lookup through its search tree
// and data section.
func verifyGeoIPDatabase(path, databaseType string) error {
	db, err := geoip2.Open(path)
	if db != nil {
		// An unknown database type still returns an open reader.
		defer func() { _ = db.Close() }()
	}
	if err != nil {
		return fmt.Errorf("downloaded GeoIP database is invalid: %w", err)
	}
	if got := db.Metadata().DatabaseType; got != databaseType {
		return fmt.Errorf("downloaded GeoIP database type is %q, want %q", got, databaseType)
	}
	probe := net.IPv4(1, 1, 1, 1)
	if databaseType == GeoIPASNDBType {
		_, err = db.ASN(probe)
	} else {
		_, err = db.City(probe)
	}
	if err != nil {
		return fmt.Errorf("downloaded GeoIP database failed a lookup: %w", err)
	}
	return nil
}
