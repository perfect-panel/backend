package epay

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/perfect-panel/server/pkg/logger"
	paymentUtil "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/tool"
)

// ErrQueryNotSupported is returned when the payment gateway does not
// implement the order query API (e.g., returns HTTP 404).
var ErrQueryNotSupported = errors.New("gateway does not support order query API")

const (
	// ModeSubmit uses EPay's browser-facing submit.php endpoint.
	ModeSubmit = "submit"
	// ModeMAPI uses EPay's server-to-server mapi.php endpoint.
	ModeMAPI = "mapi"
)

type Client struct {
	Pid        string
	Url        string
	Key        string
	Type       string
	Mode       string
	httpClient *http.Client
}

type Order struct {
	Name      string
	OrderNo   string
	Amount    float64
	SignType  string
	NotifyUrl string
	ReturnUrl string
	ClientIP  string
	Device    string
}

// PaymentResult is the transport-neutral result of starting an EPay payment.
// Type is "url" for a browser redirect and "qr" when the caller should
// render URL as a QR code.
type PaymentResult struct {
	Type    string
	URL     string
	TradeNo string
}

type QueryResult struct {
	MerchantID string
	TradeNo    string
	OrderNo    string
	Type       string
	Money      string
	Paid       bool
	Message    string
}

type queryOrderResponse struct {
	Code       int             `json:"code"`
	Msg        string          `json:"msg"`
	TradeNo    string          `json:"trade_no"`
	OutTradeNo string          `json:"out_trade_no"`
	Type       string          `json:"type"`
	Money      string          `json:"money"`
	Pid        json.RawMessage `json:"pid"`
	Status     int             `json:"status"`
}

type mapiResponse struct {
	Code      int    `json:"code"`
	Msg       string `json:"msg"`
	TradeNo   string `json:"trade_no"`
	PayURL    string `json:"payurl"`
	QRCode    string `json:"qrcode"`
	URLScheme string `json:"urlscheme"`
}

// NewClient creates an EPay client. The optional mode keeps existing callers
// on submit mode while allowing payment configurations to opt into mapi.
func NewClient(pid, url, key string, Type string, mode ...string) *Client {
	paymentMode := ModeSubmit
	if len(mode) > 0 && strings.TrimSpace(mode[0]) != "" {
		paymentMode = strings.ToLower(strings.TrimSpace(mode[0]))
	}
	return &Client{
		Pid:  pid,
		Url:  url,
		Key:  key,
		Type: Type,
		Mode: paymentMode,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// CreatePayment starts a payment using the configured mode. Submit mode keeps
// the legacy browser redirect; mapi sends a form-encoded POST from this server.
func (c *Client) CreatePayment(ctx context.Context, order Order) (*PaymentResult, error) {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "", ModeSubmit:
		paymentURL, err := c.createPayURL(order)
		if err != nil {
			return nil, err
		}
		return &PaymentResult{Type: "url", URL: paymentURL}, nil
	case ModeMAPI:
		return c.createMAPIPayment(ctx, order)
	default:
		return nil, fmt.Errorf("unsupported EPay payment mode %q", c.Mode)
	}
}

func (c *Client) createPayURL(order Order) (string, error) {
	endpoint, err := c.endpoint("submit.php")
	if err != nil {
		return "", err
	}
	params := c.orderParams(order)
	params["sign"] = c.createSign(params)
	params["sign_type"] = "MD5"
	return endpoint.String() + "?" + encodeParams(params), nil
}

// CreatePayUrl is retained for source compatibility.
func (c *Client) CreatePayUrl(order Order) string {
	paymentURL, err := c.createPayURL(order)
	if err != nil {
		return ""
	}
	return paymentURL
}

func (c *Client) createSign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if params[k] != "" && k != "sign" && k != "sign_type" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	queryString := strings.Join(parts, "&")
	text := queryString + c.Key
	return tool.Md5Encode(text, false)
}

func (c *Client) VerifySign(params map[string]string) bool {
	expected := c.createSign(params)
	received := strings.ToLower(params["sign"])
	if len(expected) != len(received) || len(received) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(received)) == 1
}

// QueryOrder obtains authoritative payment details directly from the gateway.
// A successful HTTP response is not enough: EPay-compatible gateways use code=1
// to indicate a successful lookup and status=1 to indicate a paid order.
func (c *Client) QueryOrder(orderNo string) (*QueryResult, error) {
	if orderNo == "" {
		return nil, errors.New("order number is empty")
	}
	endpoint, err := c.endpoint("api.php")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("act", "order")
	query.Set("pid", c.Pid)
	query.Set("key", c.Key)
	query.Set("out_trade_no", orderNo)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("create gateway query request failed")
	}
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("gateway query request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrQueryNotSupported
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("query gateway returned HTTP %d", resp.StatusCode)
	}
	const maxQueryResponseSize = 1 << 20
	value, err := io.ReadAll(io.LimitReader(resp.Body, maxQueryResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}
	if len(value) > maxQueryResponseSize {
		return nil, errors.New("query response is too large")
	}
	var response queryOrderResponse
	if err := json.Unmarshal(value, &response); err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}
	if response.Code != 1 {
		return nil, fmt.Errorf("gateway order lookup failed: code=%d", response.Code)
	}
	merchantID, err := rawString(response.Pid)
	if err != nil {
		return nil, fmt.Errorf("decode merchant id: %w", err)
	}
	return &QueryResult{
		MerchantID: merchantID,
		TradeNo:    response.TradeNo,
		OrderNo:    response.OutTradeNo,
		Type:       response.Type,
		Money:      response.Money,
		Paid:       response.Status == 1,
		Message:    response.Msg,
	}, nil
}

// QueryOrderStatus is kept for callers that only need a status boolean.
func (c *Client) QueryOrderStatus(orderNo string) bool {
	result, err := c.QueryOrder(orderNo)
	if err != nil {
		logger.Error("[Epay] QueryOrderStatus error", logger.Field("orderNo", orderNo), logger.Field("error", err.Error()))
		return false
	}
	return result.Paid
}

// FormatMoney returns the exact two-decimal amount sent to an EPay-compatible
// gateway. It intentionally preserves the historical truncation behaviour.
func FormatMoney(amount float64) string {
	return tool.FormatFloat(amount, 2)
}

// ParseMoney converts a non-negative decimal amount to its integer minor unit.
// It rejects floats, exponents and values with more than two decimal places.
func ParseMoney(value string) (int64, error) {
	return paymentUtil.ParseAmount(value)
}

func rawString(value json.RawMessage) (string, error) {
	if len(value) == 0 || string(value) == "null" {
		return "", errors.New("merchant id is missing")
	}
	var text string
	if value[0] == '"' {
		if err := json.Unmarshal(value, &text); err != nil {
			return "", err
		}
		return text, nil
	}
	var number json.Number
	if err := json.Unmarshal(value, &number); err != nil {
		return "", err
	}
	return number.String(), nil
}

func (c *Client) createMAPIPayment(ctx context.Context, order Order) (*PaymentResult, error) {
	if c.Type == "" {
		return nil, errors.New("EPay mapi payment type is required")
	}
	if net.ParseIP(order.ClientIP) == nil {
		return nil, errors.New("EPay mapi client IP is invalid")
	}
	endpoint, err := c.endpoint("mapi.php")
	if err != nil {
		return nil, err
	}
	params := c.orderParams(order)
	params["clientip"] = order.ClientIP
	if order.Device != "" {
		params["device"] = order.Device
	}
	params["sign"] = c.createSign(params)
	params["sign_type"] = "MD5"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(encodeParams(params)))
	if err != nil {
		return nil, fmt.Errorf("create EPay mapi request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send EPay mapi request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("EPay mapi returned HTTP %d", resp.StatusCode)
	}
	const maxMAPIResponseSize = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMAPIResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read EPay mapi response: %w", err)
	}
	if len(body) > maxMAPIResponseSize {
		return nil, errors.New("EPay mapi response is too large")
	}
	var response mapiResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode EPay mapi response: %w", err)
	}
	if response.Code != 1 {
		return nil, fmt.Errorf("EPay mapi failed: %s", response.Msg)
	}
	result := &PaymentResult{TradeNo: response.TradeNo}
	switch {
	case response.PayURL != "":
		result.Type, result.URL = "url", response.PayURL
	case response.QRCode != "":
		result.Type, result.URL = "qr", response.QRCode
	case response.URLScheme != "":
		result.Type, result.URL = "url", response.URLScheme
	default:
		return nil, errors.New("EPay mapi response has no payment destination")
	}
	return result, nil
}

func (c *Client) endpoint(script string) (*url.URL, error) {
	endpoint, err := url.Parse(c.Url)
	if err != nil {
		return nil, fmt.Errorf("parse payment endpoint: %w", err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, errors.New("unsupported payment endpoint")
	}
	if endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("payment endpoint must not include query or fragment")
	}
	path := strings.TrimRight(endpoint.Path, "/")
	for _, knownScript := range []string{"submit.php", "mapi.php", "api.php"} {
		if strings.HasSuffix(path, "/"+knownScript) {
			path = strings.TrimSuffix(path, "/"+knownScript)
			break
		}
	}
	endpoint.Path = path + "/" + script
	endpoint.RawPath = ""
	return endpoint, nil
}

func (c *Client) orderParams(order Order) map[string]string {
	return map[string]string{
		"money":        FormatMoney(order.Amount),
		"name":         order.Name,
		"notify_url":   order.NotifyUrl,
		"out_trade_no": order.OrderNo,
		"pid":          c.Pid,
		"type":         c.Type,
		"return_url":   order.ReturnUrl,
	}
}

func encodeParams(params map[string]string) string {
	values := make(url.Values, len(params))
	for key, value := range params {
		values.Set(key, value)
	}
	return values.Encode()
}

// StructToMap converts a struct to map[string]string
func (c *Client) structToMap(order Order) map[string]string {
	return c.orderParams(order)
}
