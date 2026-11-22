// Package alibabacloud sends text messages through Alibaba Cloud SMS,
// which fills a template approved on the provider's side.
package alibabacloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/client"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v2/client"
	util "github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/perfect-panel/backend/internal/infra/integration"
)

// defaultEndpoint serves accounts whose configuration names no endpoint.
const defaultEndpoint = "dysmsapi.ap-southeast-1.aliyuncs.com"

// requestTimeout bounds the connect and the read of one call when the
// caller's context sets no earlier deadline.
const requestTimeout = 10 * time.Second

// Config is the stored provider configuration.
type Config struct {
	Access       string `json:"access"`
	Secret       string `json:"secret"`
	SignName     string `json:"sign_name"`
	Endpoint     string `json:"endpoint"`
	TemplateCode string `json:"template_code"`
}

// Client sends through one Alibaba Cloud account.
type Client struct {
	config Config
	client *dysmsapi.Client
}

// NewClient builds the SDK client for the account. A configuration the SDK
// rejects is an error rather than a client that fails every send.
func NewClient(config Config) (*Client, error) {
	return newClient(config, "")
}

// newClient lets tests talk plain HTTP to a local server; an empty protocol
// keeps the SDK's HTTPS.
func newClient(config Config, protocol string) (*Client, error) {
	cfg := &openapi.Config{
		AccessKeyId:     tea.String(config.Access),
		AccessKeySecret: tea.String(config.Secret),
		Endpoint:        tea.String(config.Endpoint),
	}
	if config.Endpoint == "" {
		cfg.Endpoint = tea.String(defaultEndpoint)
	}
	if protocol != "" {
		cfg.Protocol = tea.String(protocol)
	}
	client, err := dysmsapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("init Alibaba Cloud SMS client: %w", err)
	}
	return &Client{config: config, client: client}, nil
}

// SendTemplate sends the template approved for the account, filled with
// params, to the number. The SDK takes no context: a cancelled ctx stops the
// send before it starts, and ctx's deadline caps the call's timeouts.
func (c *Client) SendTemplate(ctx context.Context, area, mobile string, params map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	templateParam, err := json.Marshal(params)
	if err != nil {
		return err
	}
	timeout := requestTimeout
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	milliseconds := tea.Int(int(max(timeout.Milliseconds(), 1)))
	resp, err := c.client.SendSmsWithOptions(&dysmsapi.SendSmsRequest{
		PhoneNumbers:  tea.String(area + mobile),
		TemplateCode:  tea.String(c.config.TemplateCode),
		SignName:      tea.String(c.config.SignName),
		TemplateParam: tea.String(string(templateParam)),
	}, &util.RuntimeOptions{ConnectTimeout: milliseconds, ReadTimeout: milliseconds})
	if err != nil {
		// The SDK sends the number and the template parameters, the code
		// among them, in the query string and returns a failed round trip
		// as is: integration.RequestError drops the URL.
		return integration.RequestError("alibaba cloud sms", err)
	}
	if resp.Body == nil || resp.Body.Code == nil {
		return errors.New("alibaba cloud send sms failed: empty response")
	}
	if code := *resp.Body.Code; code != "OK" {
		return fmt.Errorf("alibaba cloud send sms failed, code: %s, message: %s", code, tea.StringValue(resp.Body.Message))
	}
	return nil
}
