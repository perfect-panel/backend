// Package abosend sends text messages through the Abosend HTTP API.
package abosend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/integration"
	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/pkg/random"
)

// BaseURL is the API domain of a configuration that names none.
const BaseURL = "https://smsapi.abosend.com"

// maxResponseBytes bounds how much of a provider response is read.
const maxResponseBytes = 1 << 20

// Config is the stored provider configuration.
type Config struct {
	ApiDomain string `json:"api_domain"`
	Access    string `json:"access"`
	Secret    string `json:"secret"`
	Template  string `json:"template"`
}

// Client sends through one Abosend account.
type Client struct {
	config  Config
	baseURL string
	http    *http.Client
}

type request struct {
	OrgCode    string `json:"orgCode"`
	MobileArea string `json:"mobileArea"`
	Mobile     string `json:"mobiles"`
	Content    string `json:"content"`
	Rand       string `json:"rand"`
	Sign       string `json:"sign"`
}

type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		SendCode string `json:"sendCode"`
	}
}

// NewClient sends through httpClient to the configured API domain.
func NewClient(config Config, httpClient *http.Client) *Client {
	baseURL := BaseURL
	if config.ApiDomain != "" {
		baseURL = config.ApiDomain
	}
	return &Client{config: config, baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// sign is the request signature: the uppercase MD5 of the organisation
// code, the text, the nonce and the key.
func sign(access, text, nonce, secret string) string {
	return protocolkey.Md5Encode(access+text+nonce+secret, true)
}

// SendText sends text to the number; Abosend takes the area both separately
// and as the number's prefix.
func (c *Client) SendText(ctx context.Context, area, mobile, text string) error {
	nonce := random.Key(6, 0)
	body, err := json.Marshal(request{
		OrgCode:    c.config.Access,
		MobileArea: "+" + area,
		Mobile:     area + mobile,
		Content:    text,
		Rand:       nonce,
		Sign:       sign(c.config.Access, text, nonce, c.config.Secret),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/api/sendSMS", bytes.NewReader(body))
	if err != nil {
		return integration.RequestError("abosend", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return integration.RequestError("abosend", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("send sms failed, status code: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	var result response
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if result.Code != http.StatusOK {
		return fmt.Errorf("send sms failed, code: %d, msg: %s", result.Code, result.Message)
	}
	return nil
}
