// Package geolocation locates a server address through public IP
// geolocation services, filling in a server's country and city.
package geolocation

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/perfect-panel/backend/pkg/logger"
)

const (
	ipapi   = "ipapi.co"
	ipbase  = "api.ipbase.com"
	ipwhois = "ipwhois.app"
	ipinfo  = "ipinfo.io"
)

var (
	queryUrls = map[string]bool{
		ipbase:  true,
		ipapi:   true,
		ipwhois: true,
		ipinfo:  true,
	}
	geoHTTPClient = &http.Client{Timeout: 4 * time.Second}
)

// GetRegionByIp queries the geolocation of an IP address using supported
// services, trying them in turn until one answers. Cancelling ctx aborts the
// lookups.
func GetRegionByIp(ctx context.Context, ip string) (*GeoLocationResponse, error) {
	for service, enabled := range queryUrls {
		if enabled {
			response, err := fetchGeolocation(ctx, service, ip)
			if err != nil {
				logger.WithContext(ctx).Errorw("IP geolocation lookup failed", logger.Field("service", service), logger.Field("error", err))
				continue
			}
			return response, nil
		}
	}
	return nil, errors.New("unable to fetch geolocation for the IP")
}

// fetchGeolocation sends a request to the specified service to retrieve geolocation data.
func fetchGeolocation(ctx context.Context, service, ip string) (*GeoLocationResponse, error) {
	var apiURL string

	// Construct the API URL based on the service.
	switch service {
	case ipinfo:
		apiURL = fmt.Sprintf("https://ipinfo.io/%s/json", ip)
	case ipapi:
		apiURL = fmt.Sprintf("https://ipapi.co/%s/json", ip)
	case ipbase:
		apiURL = fmt.Sprintf("https://api.ipbase.com/v1/json/%s", ip)
	case ipwhois:
		apiURL = fmt.Sprintf("https://ipwhois.app/json/%s", ip)
	default:
		return nil, fmt.Errorf("unsupported service: %s", service)
	}

	// Create the HTTP request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	setHeaders(req, service)

	// Create the HTTP client and send the request.
	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	// The body is only read; closing it cannot lose data.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("geolocation service returned HTTP %d", resp.StatusCode)
	}

	// Decompress the response body based on Content-Encoding.
	body, err := decompressResponse(resp)
	if err != nil {
		return nil, err
	}
	// Parse the JSON response into GeoLocationResponse.
	var location GeoLocationResponse
	if err := json.Unmarshal(body, &location); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Ensure compatibility between country fields.
	if location.Country == "" {
		location.Country = location.CountryName
	}

	if location.Loc != "" && strings.Contains(location.Loc, ",") {
		loc := strings.Split(location.Loc, ",")
		location.Latitude = loc[0]
		location.Longitude = loc[1]
	}

	return &location, nil
}

// setHeaders sets the necessary headers for the HTTP request.
func setHeaders(req *http.Request, host string) {
	req.Header.Set("Host", host)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:134.0) Gecko/20100101 Firefox/134.0")
	req.Header.Set("Accept", "application/json, text/html, application/xhtml+xml, */*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

// maxResponseSize bounds the decoded geolocation response, whatever its
// encoding: a lookup answer is a few hundred bytes.
const maxResponseSize = 1 << 20

// decompressResponse decompresses the HTTP response body based on its Content-Encoding.
func decompressResponse(resp *http.Response) ([]byte, error) {
	var reader io.ReadCloser
	var err error

	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
	case "br":
		reader = io.NopCloser(brotli.NewReader(resp.Body))
	case "zstd":
		decoder, zstdErr := zstd.NewReader(resp.Body)
		if zstdErr != nil {
			return nil, fmt.Errorf("failed to create zstd decoder: %w", zstdErr)
		}
		defer decoder.Close()
		return io.ReadAll(io.LimitReader(decoder, maxResponseSize))
	default:
		reader = resp.Body
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}
	// The reader is only read; closing it cannot lose data.
	defer func() { _ = reader.Close() }()

	return io.ReadAll(io.LimitReader(reader, maxResponseSize))
}

// GeoLocationResponse represents the geolocation data returned by the API.
type GeoLocationResponse struct {
	Country     string `json:"country"`
	CountryName string `json:"country_name"`
	Region      string `json:"region"`
	City        string `json:"city"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
	Loc         string `json:"loc"`
}
