// Package twilio sends text messages through the Twilio API.
package twilio

import (
	"context"
	"fmt"
	"net/http"

	"github.com/perfect-panel/backend/internal/infra/integration"
	"github.com/twilio/twilio-go/client"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

// Config is the stored provider configuration.
type Config struct {
	Access      string `json:"access"`
	Secret      string `json:"secret"`
	PhoneNumber string `json:"phone_number"`
	Template    string `json:"template"`
}

// Client sends through one Twilio account.
type Client struct {
	config Config
	http   *http.Client
}

// NewClient sends through httpClient's transport, so the connections to
// Twilio are pooled with the other providers'.
func NewClient(config Config, httpClient *http.Client) *Client {
	return &Client{config: config, http: httpClient}
}

// SendText sends text to the number, which Twilio takes in E.164 form.
func (c *Client) SendText(ctx context.Context, area, mobile, text string) error {
	params := &twilioApi.CreateMessageParams{}
	params.SetTo(fmt.Sprintf("+%s%s", area, mobile))
	params.SetFrom(c.config.PhoneNumber)
	params.SetBody(text)
	resp, err := c.api(ctx).CreateMessage(params)
	if err != nil {
		// The SDK returns a failed round trip as is, and its URL names the
		// account: integration.RequestError drops it.
		return integration.RequestError("twilio", err)
	}
	if resp.ErrorCode != nil {
		message := ""
		if resp.ErrorMessage != nil {
			message = *resp.ErrorMessage
		}
		return fmt.Errorf("twilio send code error: %d %s", *resp.ErrorCode, message)
	}
	return nil
}

// api builds the SDK service for one call. The SDK takes no context, so the
// call's HTTP client carries it to every request the SDK makes.
func (c *Client) api(ctx context.Context) *twilioApi.ApiService {
	base := &client.Client{
		Credentials: client.NewCredentials(c.config.Access, c.config.Secret),
		HTTPClient: &http.Client{
			Transport: contextTransport{ctx: ctx, base: c.transport()},
			Timeout:   c.http.Timeout,
			// Like the SDK's own client: report a redirect, never follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	base.SetAccountSid(c.config.Access)
	return twilioApi.NewApiServiceWithClient(base)
}

func (c *Client) transport() http.RoundTripper {
	if c.http.Transport != nil {
		return c.http.Transport
	}
	return http.DefaultTransport
}

// contextTransport sends every request under ctx.
type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Context() != t.ctx {
		req = req.WithContext(t.ctx)
	}
	return t.base.RoundTrip(req)
}
