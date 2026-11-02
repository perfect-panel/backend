package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type V1Config struct {
	BaseURL    string // Required HTTPS origin of the target EPay deployment, optionally with a deployment path.
	MerchantID string
	Key        string // V1 shared merchant KEY, not a V2 RSA key.
	HTTPClient *http.Client
}

// V1Client implements EPay V1. It never falls back to V2 or retries requests.
type V1Client struct {
	baseURL    string
	merchantID string
	key        string
	http       *http.Client
}

func NewV1Client(config V1Config) (*V1Client, error) {
	if config.MerchantID == "" || strings.Trim(config.MerchantID, "0123456789") != "" {
		return nil, errors.New("epay v1: MerchantID must contain decimal digits")
	}
	if config.BaseURL == "" {
		return nil, errors.New("epay v1: BaseURL is required")
	}
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !validBasePath(u) || strings.HasSuffix(strings.ToLower(strings.TrimSuffix(u.Path, "/")), ".php") {
		return nil, errors.New("epay v1: BaseURL must be an HTTPS origin or deployment path without credentials, script name, query or fragment")
	}
	if strings.TrimSpace(config.Key) == "" {
		return nil, errors.New("epay v1: merchant Key is required")
	}
	httpClient := http.Client{Timeout: 15 * time.Second}
	if config.HTTPClient != nil {
		httpClient = *config.HTTPClient
		if httpClient.Timeout == 0 {
			httpClient.Timeout = 15 * time.Second
		}
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &V1Client{
		baseURL: u.Scheme + "://" + u.Host + strings.TrimSuffix(u.Path, "/"), merchantID: config.MerchantID,
		key: config.Key, http: &httpClient,
	}, nil
}

// V1Response has passed JSON and API success-code checks only. V1 does not define
// signed JSON responses; HTTPS authenticates transport. MerchantInfo may include
// the shared key, so never log the complete response.
type V1Response struct{ raw []byte }

func (r *V1Response) Decode(destination any) error { return json.Unmarshal(r.raw, destination) }
func (r *V1Response) JSON() []byte                 { return append([]byte(nil), r.raw...) }

// V1APIError identifies an API failure using the operation-specific success code.
// In particular code=0 fails ordinary V1 requests but succeeds for refunds.
type V1APIError struct {
	Code    int
	Message string
}

func (e *V1APIError) Error() string {
	return fmt.Sprintf("epay v1: API code=%d: %s", e.Code, e.Message)
}

// net/http URL errors can contain the V1 KEY in query strings. Keep the printable
// error generic while preserving errors.Is/As for callers that need the cause.
type v1RequestError struct{ cause error }

func (e *v1RequestError) Error() string { return "epay v1: request failed; outcome may be unknown" }
func (e *v1RequestError) Unwrap() error { return e.cause }

func (c *V1Client) send(ctx context.Context, method, endpoint string, form url.Values, successCode int) (*V1Response, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, &v1RequestError{cause: err}
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, &v1RequestError{cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &v1RequestError{cause: err}
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("epay v1: response exceeds 2 MiB")
	}
	return c.parseResponse(data, successCode)
}

func (c *V1Client) parseResponse(data []byte, successCode int) (*V1Response, error) {
	fields, err := responseObject(data)
	if err != nil {
		// Do not include server bytes: a V1 response can contain the shared key.
		return nil, errors.New("epay v1: invalid or ambiguous JSON response")
	}
	code, err := v1Integer(fields["code"])
	if err != nil {
		return nil, errors.New("epay v1: missing or invalid response code")
	}
	if code != successCode {
		var message string
		_ = json.Unmarshal(fields["msg"], &message)
		message = strings.ReplaceAll(message, c.key, "[redacted]")
		return nil, &V1APIError{Code: code, Message: message}
	}
	return &V1Response{raw: append([]byte(nil), data...)}, nil
}

func v1Scalar(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	if jsonInteger.Match(raw) {
		return string(raw), nil
	}
	return "", errors.New("epay v1: expected a string or integer")
}

func v1Integer(raw json.RawMessage) (int, error) {
	value, err := v1Scalar(raw)
	if err != nil || !jsonInteger.MatchString(value) {
		return 0, errors.New("epay v1: expected an integer or integer string")
	}
	return strconv.Atoi(value)
}

func v1Values(params Params) url.Values {
	values := make(url.Values, len(params))
	for key, value := range params {
		values.Set(key, value)
	}
	return values
}

// queryAPI is deliberately private and limited to documented read operations.
func (c *V1Client) queryAPI(ctx context.Context, action string, params Params) (*V1Response, error) {
	switch action {
	case "query", "settle", "order", "orders":
	default:
		return nil, errors.New("epay v1: unsupported query operation")
	}
	values := v1Values(params)
	values.Set("act", action)
	values.Set("pid", c.merchantID)
	values.Set("key", c.key)
	return c.send(ctx, http.MethodGet, c.baseURL+"/api.php?"+values.Encode(), nil, 1)
}

// MerchantInfo returns account data, including KEY on some deployments. Decode
// only the fields needed by the application instead of logging JSON().
func (c *V1Client) MerchantInfo(ctx context.Context) (*V1Response, error) {
	return c.queryAPI(ctx, "query", nil)
}

// Settlements returns the deployment's settlement records in data. The reference
// document does not specify their row schema; use Decode with a site-specific type.
func (c *V1Client) Settlements(ctx context.Context) (*V1Response, error) {
	return c.queryAPI(ctx, "settle", nil)
}

type V1OrdersRequest struct {
	Page  int // Zero selects page 1.
	Limit int // Zero selects 20; maximum 50.
}

func (c *V1Client) Orders(ctx context.Context, request V1OrdersRequest) (*V1Response, error) {
	if request.Page < 0 || request.Limit < 0 || request.Limit > 50 {
		return nil, errors.New("epay v1: page must be positive and limit must be between 1 and 50")
	}
	if request.Page == 0 {
		request.Page = 1
	}
	if request.Limit == 0 {
		request.Limit = 20
	}
	return c.queryAPI(ctx, "orders", Params{"page": strconv.Itoa(request.Page), "limit": strconv.Itoa(request.Limit)})
}
