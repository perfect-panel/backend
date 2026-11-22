package paypal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

const (
	Approved = "CHECKOUT.ORDER.APPROVED"
	Captured = "PAYMENT.CAPTURE.COMPLETED"
)

var ErrInvalidWebhook = errors.New("invalid PayPal webhook")

// Transmission contains the five PayPal delivery headers passed to the
// official verification API. The certificate URL is never fetched locally.
type Transmission struct {
	ID        string `json:"transmission_id"`
	Time      string `json:"transmission_time"`
	Signature string `json:"transmission_sig"`
	CertURL   string `json:"cert_url"`
	Algorithm string `json:"auth_algo"`
}

type Event struct {
	Type     string `json:"event_type"`
	Resource struct {
		ID      string `json:"id"`
		Related struct {
			IDs struct {
				OrderID string `json:"order_id"`
			} `json:"related_ids"`
		} `json:"supplementary_data"`
	} `json:"resource"`
}

func (c *Client) Verify(ctx context.Context, t Transmission, body []byte) (*Event, error) {
	if t.ID == "" || t.Time == "" || t.Signature == "" || t.CertURL == "" || t.Algorithm == "" {
		return nil, ErrInvalidWebhook
	}
	var event Event
	if err := json.Unmarshal(body, &event); err != nil || event.Type == "" {
		return nil, ErrInvalidWebhook
	}
	// Preserve the signed event bytes, including whitespace and HTML characters.
	prefix, err := json.Marshal(struct {
		Transmission
		WebhookID string `json:"webhook_id"`
	}{t, c.webhookID})
	if err != nil {
		return nil, err
	}
	payload := append(prefix[:len(prefix)-1], `,"webhook_event":`...)
	payload = append(payload, body...)
	payload = append(payload, '}')
	var result struct {
		Status string `json:"verification_status"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", "", payload, &result); err != nil {
		return nil, err
	}
	if result.Status != "SUCCESS" {
		return nil, ErrInvalidWebhook
	}
	return &event, nil
}
