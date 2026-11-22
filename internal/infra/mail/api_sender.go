package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	stdmail "net/mail"
	"regexp"
	"strings"
)

// apiSender holds one account's immutable settings. The two outbound JSON
// APIs share the envelope but use different sender formats and receipts.
type apiSender struct {
	platform Platform
	endpoint string
	token    string
	from     any
	replyTo  string
	client   *http.Client
}

var accountIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func newAPISender(platform Platform, config, siteName string) (Sender, error) {
	var cfg struct {
		From      string `json:"from"`
		ReplyTo   string `json:"reply_to"`
		APIKey    string `json:"api_key"`
		AccountID string `json:"account_id"`
		APIToken  string `json:"api_token"`
	}
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return nil, fmt.Errorf("%s: invalid provider configuration", platform)
	}
	from, err := stdmail.ParseAddress(strings.TrimSpace(cfg.From))
	if err != nil {
		return nil, fmt.Errorf("%s: a valid from address is required", platform)
	}
	sender := &apiSender{
		platform: platform,
		from:     from.Address,
		client: &http.Client{
			Timeout:       sendTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	if replyTo := strings.TrimSpace(cfg.ReplyTo); replyTo != "" {
		address, err := stdmail.ParseAddress(replyTo)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid reply_to address", platform)
		}
		sender.replyTo = address.Address
	}
	if platform == Resend {
		sender.endpoint = "https://api.resend.com/emails"
		sender.token = strings.TrimSpace(cfg.APIKey)
		if siteName != "" {
			from.Name = siteName
			sender.from = from.String()
		}
	} else {
		accountID := strings.TrimSpace(cfg.AccountID)
		if !accountIDPattern.MatchString(accountID) {
			return nil, errors.New("cloudflare: a valid account_id is required")
		}
		sender.endpoint = "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/email/sending/send"
		sender.token = strings.TrimSpace(cfg.APIToken)
		if siteName != "" {
			sender.from = map[string]string{"address": from.Address, "name": siteName}
		}
	}
	if sender.token == "" || strings.ContainsAny(sender.token, " \t\r\n") {
		return nil, fmt.Errorf("%s: missing or invalid API credential", platform)
	}
	return sender, nil
}

func (s *apiSender) SendContext(ctx context.Context, to []string, subject, html string) error {
	if len(to) == 0 || len(to) > 50 {
		return fmt.Errorf("%s: expected 1 to 50 recipients", s.platform)
	}
	payload, err := json.Marshal(struct {
		From    any      `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html"`
		ReplyTo string   `json:"reply_to,omitempty"`
	}{s.from, to, subject, html, s.replyTo})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("User-Agent", "PPanel")
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s: send request timed out: %w", s.platform, context.DeadlineExceeded)
		}
		return fmt.Errorf("%s: could not reach the email API", s.platform)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The provider's error message can echo recipient addresses or content.
		return fmt.Errorf("%s: send returned HTTP %d", s.platform, resp.StatusCode)
	}
	const maxReceipt = 64 << 10
	receipt, err := io.ReadAll(io.LimitReader(resp.Body, maxReceipt+1))
	if err != nil || len(receipt) > maxReceipt {
		return fmt.Errorf("%s: could not read send receipt", s.platform)
	}
	if s.platform == Resend {
		var result struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(receipt, &result) != nil || result.ID == "" {
			return errors.New("resend: missing message ID in send receipt")
		}
		return nil
	}
	return checkCloudflareReceipt(receipt, len(to))
}

func checkCloudflareReceipt(receipt []byte, recipients int) error {
	var cf struct {
		Success bool `json:"success"`
		Result  struct {
			MessageID  string   `json:"message_id"`
			Delivered  []string `json:"delivered"`
			Queued     []string `json:"queued"`
			Bounced    []string `json:"permanent_bounces"`
			Suppressed []string `json:"suppressed_recipients"`
		} `json:"result"`
	}
	if json.Unmarshal(receipt, &cf) != nil || !cf.Success || cf.Result.MessageID == "" {
		return errors.New("cloudflare: invalid or unsuccessful send receipt")
	}
	if len(cf.Result.Bounced)+len(cf.Result.Suppressed) > 0 {
		return fmt.Errorf("cloudflare: bounced: %d, suppressed: %d", len(cf.Result.Bounced), len(cf.Result.Suppressed))
	}
	if len(cf.Result.Queued)+len(cf.Result.Delivered) != recipients {
		return errors.New("cloudflare: incomplete recipient status in send receipt")
	}
	return nil
}
