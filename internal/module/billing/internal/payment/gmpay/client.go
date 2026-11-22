// Package gmpay implements the native GMPay v1 merchant protocol.
package gmpay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
)

type Config struct {
	URL    string `json:"url"`
	PID    string `json:"pid"`
	Secret string `json:"secret_key"`
}

func (c *Config) Validate() error {
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.URL, "#") ||
		u.RawPath != "" || (u.Path != "" && path.Clean(u.Path) != u.Path) {
		return errors.New("GMPay requires an HTTPS gateway base URL")
	}
	c.PID = strings.TrimSpace(c.PID)
	if c.PID == "" || strings.Contains(c.PID, "&") || strings.TrimSpace(c.Secret) == "" {
		return errors.New("GMPay requires a merchant PID and secret_key")
	}
	return nil
}

type Client struct {
	config Config
	http   *http.Client
}

func NewClient(cfg Config, transport *http.Client) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var h http.Client
	if transport != nil {
		h = *transport
	}
	if h.Timeout == 0 {
		h.Timeout = 15 * time.Second
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{config: cfg, http: &h}, nil
}

const (
	Paid    = 2
	Expired = 3
)

var tradeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,255}$`)

// Cashier is the reusable entry returned by a successful create.
type Cashier struct {
	TradeID    string `json:"trade_id"`
	PaymentURL string `json:"payment_url"`
}

func (c Cashier) Valid() bool {
	return tradeIDPattern.MatchString(c.TradeID) && validWebURL(c.PaymentURL, true)
}

// Order contains the fiat charge. The provider's actual_amount is denominated
// in crypto and does not enter PPanel's settlement calculations.
type Order struct {
	Cashier
	OrderID     string      `json:"order_id"`
	Amount      json.Number `json:"amount"`
	Currency    string      `json:"currency"`
	PaymentType string      `json:"payment_type"`
	Status      int         `json:"status"`
}

func (o *Order) MatchesCharge(amount int64, currency string) bool {
	cents, err := parseFiat(o.Amount.String())
	return err == nil && cents == amount && cents > 0 && currency != "" && strings.EqualFold(o.Currency, currency)
}

func parseFiat(raw string) (int64, error) {
	if strings.Contains(raw, ".") {
		raw = strings.TrimSuffix(strings.TrimRight(raw, "0"), ".")
	}
	return payment.ParseAmount(raw)
}

type CreateRequest struct {
	OrderID, Currency, Subject, NotifyURL, ReturnURL string
	Amount                                           int64
}

func (r CreateRequest) Validate() error {
	if r.OrderID == "" || len(r.OrderID) > 32 || r.Amount <= 1 || r.Currency == "" {
		return errors.New("GMPay requires an order ID of up to 32 bytes and a fiat amount above 0.01")
	}
	if !validWebURL(r.NotifyURL, false) || (r.ReturnURL != "" && !validWebURL(r.ReturnURL, false)) {
		return errors.New("invalid GMPay notification or return URL")
	}
	return nil
}

func validWebURL(raw string, httpsOnly bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && (u.Scheme == "https" || (!httpsOnly && u.Scheme == "http"))
}

func (c *Client) Create(ctx context.Context, r CreateRequest) (*Order, error) {
	// Form values keep the exact decimal representation used for the HMAC.
	params := map[string]string{
		"pid":          c.config.PID,
		"order_id":     r.OrderID,
		"amount":       payment.FormatAmount(r.Amount),
		"currency":     strings.ToLower(r.Currency),
		"notify_url":   r.NotifyURL,
		"redirect_url": r.ReturnURL,
		"name":         r.Subject,
	}
	form := url.Values{"signature": {signature(params, c.config.Secret)}}
	for key, value := range params {
		if value != "" {
			form.Set(key, value)
		}
	}
	result, err := c.request(ctx, http.MethodPost, "/payments/gmpay/v1/order/create-transaction", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	if result.OrderID != r.OrderID || !result.MatchesCharge(r.Amount, r.Currency) || !result.Cashier.Valid() ||
		(result.Status != 1 && result.Status != 4) {
		return nil, errors.New("GMPay create response does not match checkout")
	}
	return result, nil
}

func (c *Client) Detail(ctx context.Context, tradeID string) (*Order, error) {
	if !tradeIDPattern.MatchString(tradeID) {
		return nil, errors.New("invalid GMPay trade ID")
	}
	result, err := c.request(ctx, http.MethodGet, "/pay/checkout-counter-resp/"+tradeID, nil)
	if err != nil {
		return nil, err
	}
	if result.TradeID != tradeID || !strings.EqualFold(result.PaymentType, "gmpay") {
		return nil, errors.New("GMPay cashier returned a different transaction or payment type")
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body io.Reader) (*Order, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.config.URL+endpoint, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GMPay HTTP status %d", resp.StatusCode)
	}
	const limit = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, errors.New("GMPay response too large")
	}
	var result struct {
		Code int    `json:"status_code"`
		Data *Order `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 || result.Data == nil {
		return nil, fmt.Errorf("invalid GMPay response (status_code %d)", result.Code)
	}
	return result.Data, nil
}
