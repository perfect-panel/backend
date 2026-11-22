// Package smsbao sends text messages through the SMSBao HTTP API.
package smsbao

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/integration"
	"github.com/perfect-panel/backend/internal/infra/protocolkey"
)

// BaseURL is the API the client sends to.
const BaseURL = "https://api.smsbao.com"

// maxResponseBytes bounds how much of a provider response is read; the
// answer is a short status code.
const maxResponseBytes = 1 << 10

// Config is the stored provider configuration.
type Config struct {
	Access   string `json:"access"`
	Secret   string `json:"secret"`
	Template string `json:"template"`
}

// Client sends through one SMSBao account.
type Client struct {
	config  Config
	baseURL string
	http    *http.Client
}

// NewClient sends through httpClient.
func NewClient(config Config, httpClient *http.Client) *Client {
	return &Client{config: config, baseURL: BaseURL, http: httpClient}
}

// SendText sends text to the number. Mainland China numbers (area 86) go
// through the domestic endpoint without a prefix, every other number through
// the international one in +<area><number> form.
func (c *Client) SendText(ctx context.Context, area, mobile, text string) error {
	path := "/sms"
	number := mobile
	if area != "86" {
		path = "/wsms"
		number = fmt.Sprintf("+%s%s", area, mobile)
	}
	query := url.Values{
		"u": {c.config.Access},
		"p": {protocolkey.Md5Encode(c.config.Secret, false)},
		"m": {number},
		"c": {text},
	}
	// The account, the password hash, the number and the code travel in the
	// URL, so a failure is reported without it (integration.RequestError).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return integration.RequestError("smsbao", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return integration.RequestError("smsbao", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	return parseError([]byte(strings.TrimSpace(string(body))))
}
