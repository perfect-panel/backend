// Package epay implements the EPay payment protocol: signed payment URLs,
// callback signature verification and the order query, with the
// EasyPay-compatible query as a fallback.
package epay

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/epay/protocol"
)

// ErrQueryNotSupported is returned when the payment gateway implements
// neither the standard EPay order query API nor the EasyPay-compatible
// fallback API — the endpoint returns HTTP 404 or answers with something
// that is not JSON at all (e.g. an HTML error page).
var ErrQueryNotSupported = errors.New("gateway does not support order query API")

const defaultTimeout = 5 * time.Second

// decodeQueryResponse parses a gateway query payload. A body that is not
// JSON at all is evidence the gateway does not implement the query protocol,
// so it maps to ErrQueryNotSupported instead of a fatal decode error —
// otherwise a gateway serving HTML error pages on the query path would also
// block signature-verified callbacks and close attempts.
func decodeQueryResponse(name string, body []byte, target any) error {
	if err := json.Unmarshal(body, target); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return fmt.Errorf("%s query returned a non-JSON response: %w", name, ErrQueryNotSupported)
		}
		return fmt.Errorf("decode %s query response: %w", name, err)
	}
	return nil
}

type Client struct {
	Pid        string
	Url        string
	Key        string
	Type       string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sends the gateway queries through client; nil keeps the
// default client with a five-second timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// Order is a payment to redirect the buyer to. Amount is in minor units and
// is sent to the gateway exactly, as FormatAmount renders it.
type Order struct {
	Name      string
	OrderNo   string
	Amount    int64
	SignType  string
	NotifyUrl string
	ReturnUrl string
}

type QueryResult struct {
	MerchantID string
	TradeNo    string
	OrderNo    string
	Type       string
	Money      string
	Paid       bool
	// Unpaid reports that the gateway explicitly lists the order as awaiting
	// payment (standard status 0). A result that is neither Paid nor Unpaid,
	// such as a refunded or frozen order or an unrecognised status-only
	// answer, leaves the payment state unknown.
	Unpaid  bool
	Message string
	// StatusOnly reports that the gateway query API returned only a payment
	// status. Callers must not treat omitted payment details as verified.
	StatusOnly bool
}

type queryOrderResponse struct {
	Code       queryInteger    `json:"code"`
	Msg        string          `json:"msg"`
	TradeNo    string          `json:"trade_no"`
	OutTradeNo string          `json:"out_trade_no"`
	Type       string          `json:"type"`
	Money      string          `json:"money"`
	Pid        json.RawMessage `json:"pid"`
	// Status is a pointer so an omitted status is not read as 0 (unpaid).
	Status *queryInteger `json:"status"`
}

// PHP deployments serialize these scalars as either numbers or integer strings.
// A missing status stays nil, never an implicit unpaid result.
type queryInteger int

func (i *queryInteger) UnmarshalJSON(raw []byte) error {
	value, err := rawString(raw)
	if err != nil {
		return err
	}
	number, err := strconv.Atoi(value)
	if err != nil || strconv.Itoa(number) != value {
		return errors.New("invalid gateway integer")
	}
	*i = queryInteger(number)
	return nil
}

// easyPayQueryOrderResponse is used by a non-standard EPay variant. Its
// query response intentionally contains only an order status, rather than
// the full payment details returned by standard EPay's api.php endpoint.
type easyPayQueryOrderResponse struct {
	Code queryInteger `json:"code"`
	Msg  string       `json:"msg"`
	Data *struct {
		Status string `json:"status"`
	} `json:"data"`
}

func NewClient(pid, url, key string, Type string, opts ...Option) *Client {
	client := &Client{
		Pid:  pid,
		Url:  url,
		Key:  key,
		Type: Type,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(client)
	}
	// Keep caller-owned clients unchanged and never follow a query redirect.
	httpClient := *client.httpClient
	if httpClient.Timeout == 0 {
		httpClient.Timeout = defaultTimeout
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.httpClient = &httpClient
	return client
}

// Validate checks legacy V1 configuration without contacting the gateway.
// HTTP and deployment subpaths remain supported for existing installations.
// The gateway resolves site/scheme-relative forms before making requests.
func (c *Client) Validate() error {
	if c.Pid == "" || strings.Trim(c.Pid, "0123456789") != "" || strings.TrimSpace(c.Key) == "" {
		return errors.New("EPay V1 requires a numeric merchant ID and merchant key")
	}
	switch c.Type {
	case "", "alipay", "wxpay", "qqpay", "bank", "jdpay", "paypal", "douyinpay":
	default:
		return errors.New("unsupported EPay V1 payment type")
	}
	if strings.HasPrefix(c.Url, "/") && !strings.HasPrefix(c.Url, "//") {
		parsed, err := url.Parse(c.Url)
		if err != nil || parsed.Host != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return errors.New("invalid site-relative payment endpoint")
		}
		return nil
	}
	endpointClient := *c
	if strings.HasPrefix(c.Url, "//") {
		endpointClient.Url = "https:" + c.Url
	}
	_, err := endpointClient.endpoint("submit.php")
	return err
}

// CreatePayUrl builds the browser redirect URL for EPay's submit.php endpoint.
func (c *Client) CreatePayUrl(order Order) (string, error) {
	endpoint, err := c.endpoint("submit.php")
	if err != nil {
		return "", err
	}
	params := c.orderParams(order)
	params["sign"] = c.createSign(params)
	params["sign_type"] = "MD5"
	return endpoint.String() + "?" + encodeParams(params), nil
}

func (c *Client) createSign(params map[string]string) string {
	canonical, err := protocol.V1CanonicalString(params)
	if err != nil {
		return ""
	}
	text := canonical + c.Key
	return protocolkey.Md5Encode(text, false)
}

func (c *Client) VerifySign(params map[string]string) bool {
	expected := c.createSign(params)
	received := strings.ToLower(params["sign"])
	if len(expected) != len(received) || len(received) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(received)) == 1
}

// QueryOrder obtains payment details directly from the gateway. It first uses
// the standard EPay api.php protocol. When that endpoint is absent, it falls
// back to a known EasyPay-compatible POST protocol that exposes only status.
func (c *Client) QueryOrder(ctx context.Context, orderNo string) (*QueryResult, error) {
	if orderNo == "" {
		return nil, errors.New("order number is empty")
	}
	result, err := c.queryStandardOrder(ctx, orderNo)
	if !errors.Is(err, ErrQueryNotSupported) {
		return result, err
	}
	return c.queryEasyPayOrder(ctx, orderNo)
}

// queryStandardOrder implements EPay's GET api.php?act=order protocol.
func (c *Client) queryStandardOrder(ctx context.Context, orderNo string) (*QueryResult, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("create gateway query request failed")
	}
	value, err := c.do(req, "gateway")
	if err != nil {
		return nil, err
	}
	var response queryOrderResponse
	if err := decodeQueryResponse("gateway", value, &response); err != nil {
		return nil, err
	}
	if response.Code != 1 {
		return nil, fmt.Errorf("gateway order lookup failed: code=%d", response.Code)
	}
	merchantID, err := rawString(response.Pid)
	if err != nil {
		return nil, fmt.Errorf("decode merchant id: %w", err)
	}
	if merchantID != c.Pid || response.OutTradeNo != orderNo {
		return nil, errors.New("gateway query merchant or order mismatch")
	}
	return &QueryResult{
		MerchantID: merchantID,
		TradeNo:    response.TradeNo,
		OrderNo:    response.OutTradeNo,
		Type:       response.Type,
		Money:      response.Money,
		Paid:       response.Status != nil && *response.Status == 1,
		Unpaid:     response.Status != nil && *response.Status == 0,
		Message:    response.Msg,
	}, nil
}

// queryEasyPayOrder implements the status-only fallback used by a known
// modified EPay gateway. It is attempted only after standard api.php returns
// 404, so existing EPay integrations keep their normal protocol.
func (c *Client) queryEasyPayOrder(ctx context.Context, orderNo string) (*QueryResult, error) {
	endpoint, err := c.endpoint("api/EasyPay/queryOrder")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(url.Values{
		"orderNo": []string{orderNo},
	}.Encode()))
	if err != nil {
		return nil, errors.New("create EasyPay query request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	value, err := c.do(req, "EasyPay")
	if err != nil {
		return nil, err
	}
	var response easyPayQueryOrderResponse
	if err := decodeQueryResponse("EasyPay", value, &response); err != nil {
		return nil, err
	}
	if response.Code != 1 {
		return nil, fmt.Errorf("EasyPay order lookup failed: code=%d", response.Code)
	}
	if response.Data == nil || strings.TrimSpace(response.Data.Status) == "" {
		return nil, errors.New("EasyPay query response has no order status")
	}
	return &QueryResult{
		Paid:       strings.EqualFold(response.Data.Status, "success"),
		Message:    response.Msg,
		StatusOnly: true,
	}, nil
}

// do sends a query and returns its body. A 404 means the query protocol is
// not implemented; any other non-2xx status is a gateway failure.
func (c *Client) do(req *http.Request, name string) ([]byte, error) {
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s query request failed", name)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrQueryNotSupported
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s query returned HTTP %d", name, resp.StatusCode)
	}
	const maxQueryResponseSize = 1 << 20
	value, err := io.ReadAll(io.LimitReader(resp.Body, maxQueryResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s query response: %w", name, err)
	}
	if len(value) > maxQueryResponseSize {
		return nil, fmt.Errorf("%s query response is too large", name)
	}
	return value, nil
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

func (c *Client) endpoint(script string) (*url.URL, error) {
	endpoint, err := url.Parse(c.Url)
	if err != nil {
		return nil, fmt.Errorf("parse payment endpoint: %w", err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, errors.New("unsupported payment endpoint")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return nil, errors.New("payment endpoint must not include credentials, query or fragment")
	}
	path := strings.TrimRight(endpoint.Path, "/")
	for _, knownScript := range []string{"submit.php", "api.php"} {
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
		"money":        payment.FormatAmount(order.Amount),
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
