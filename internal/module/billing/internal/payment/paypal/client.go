// Package paypal implements single-merchant PayPal Checkout with Orders v2.
package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// PayPal Checkout accepts a two-letter language and two-letter region.
var checkoutLocale = regexp.MustCompile(`^[A-Za-z]{2}-[A-Za-z]{2}$`)

type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	WebhookID    string `json:"webhook_id,omitempty"`
	Environment  string `json:"environment"`
	Locale       string `json:"locale,omitempty"`
}

func (c *Config) Validate() error {
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.ClientSecret = strings.TrimSpace(c.ClientSecret)
	c.WebhookID = strings.TrimSpace(c.WebhookID)
	c.Environment = strings.ToLower(strings.TrimSpace(c.Environment))
	if c.Environment == "" {
		c.Environment = "live"
	}
	if c.Environment != "live" && c.Environment != "sandbox" {
		return errors.New("PayPal environment must be live or sandbox")
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("PayPal client_id and client_secret are required")
	}
	c.Locale = strings.TrimSpace(c.Locale)
	if c.Locale != "" {
		if !checkoutLocale.MatchString(c.Locale) {
			return errors.New("PayPal locale must be a language-region tag such as zh-CN or en-US")
		}
		c.Locale = strings.ToLower(c.Locale[:2]) + "-" + strings.ToUpper(c.Locale[3:])
	}
	return nil
}

type Client struct {
	base      string
	http      *http.Client
	auth      clientcredentials.Config
	webhookID string
	locale    string
	mu        sync.Mutex
	token     *oauth2.Token
}

func NewClient(cfg Config, h *http.Client) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.WebhookID == "" {
		return nil, errors.New("PayPal webhook_id is required to accept payments")
	}
	base := "https://api-m.paypal.com"
	if cfg.Environment == "sandbox" {
		base = "https://api-m.sandbox.paypal.com"
	}
	var client http.Client
	if h != nil {
		client = *h
	}
	if client.Timeout == 0 {
		client.Timeout = 15 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, http: &client, webhookID: cfg.WebhookID, locale: cfg.Locale, auth: clientcredentials.Config{
		ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, TokenURL: base + "/v1/oauth2/token", AuthStyle: oauth2.AuthStyleInHeader,
	}}, nil
}

func (c *Client) accessToken(ctx context.Context) (*oauth2.Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token.Valid() {
		return c.token, nil
	}
	token, err := c.auth.Token(context.WithValue(ctx, oauth2.HTTPClient, c.http))
	if err != nil {
		return nil, errors.New("could not obtain PayPal access token")
	}
	c.token = token
	return token, nil
}

func (c *Client) request(ctx context.Context, method, path, idempotencyKey string, body []byte, response any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	token.SetAuthHeader(req)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("PayPal-Request-Id", idempotencyKey)
		req.Header.Set("Prefer", "return=representation")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("PayPal request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound && method == http.MethodGet {
		var apiError struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&apiError); err == nil && apiError.Name == "RESOURCE_NOT_FOUND" {
			return ErrOrderNotFound
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("PayPal API returned HTTP %d", res.StatusCode)
	}
	const maxBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxBytes {
		return errors.New("PayPal response exceeds size limit")
	}
	return json.Unmarshal(raw, response)
}
