package epay

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type Client struct {
	Pid        string
	Url        string
	Key        string
	Type       string
	httpClient *http.Client
}

type Order struct {
	Name      string
	OrderNo   string
	Amount    float64
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

func NewClient(pid, url, key string, Type string) *Client {
	return &Client{
		Pid:  pid,
		Url:  url,
		Key:  key,
		Type: Type,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) CreatePayUrl(order Order) string {
	// Prepare URL values
	params := url.Values{}
	params.Set("name", order.Name)
	params.Set("money", FormatMoney(order.Amount))
	params.Set("notify_url", order.NotifyUrl)
	params.Set("out_trade_no", order.OrderNo)
	params.Set("pid", c.Pid)
	params.Set("type", c.Type)
	params.Set("return_url", order.ReturnUrl)

	// Generate the sign using the CreateSign function
	sign := c.createSign(c.structToMap(order))
	params.Set("sign", sign)

	// Add sign_type manually
	params.Set("sign_type", "MD5")
	return c.Url + "/submit.php?" + params.Encode()
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
	endpoint, err := url.Parse(c.Url)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, errors.New("unsupported payment endpoint scheme")
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/api.php"
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

// StructToMap converts a struct to map[string]string
func (c *Client) structToMap(order Order) map[string]string {
	result := make(map[string]string)
	result["money"] = FormatMoney(order.Amount)
	result["name"] = order.Name
	result["notify_url"] = order.NotifyUrl
	result["out_trade_no"] = order.OrderNo
	result["pid"] = c.Pid
	result["type"] = c.Type
	result["return_url"] = order.ReturnUrl
	return result
}
