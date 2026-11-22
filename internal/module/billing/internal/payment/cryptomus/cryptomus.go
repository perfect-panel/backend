// Package cryptomus implements the Cryptomus crypto-payment gateway protocol:
// invoice creation, invoice lookup and webhook signature verification.
// https://doc.cryptomus.com/
package cryptomus

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
)

const (
	DefaultBaseURL = "https://api.cryptomus.com"

	createInvoicePath = "/v1/payment"
	invoiceInfoPath   = "/v1/payment/info"

	maxResponseSize = 1 << 20
)

// Payment statuses documented by Cryptomus. paid_over means the payer sent
// more than the invoice amount; the invoice itself is still settled in full.
// wrong_amount is a final state in which the payer sent less than the invoice
// amount, and locked is a final state in which received funds were frozen by
// the AML program: both hold money without covering the order.
const (
	StatusPaid               = "paid"
	StatusPaidOver           = "paid_over"
	StatusWrongAmount        = "wrong_amount"
	StatusWrongAmountWaiting = "wrong_amount_waiting"
	StatusProcess            = "process"
	StatusConfirmCheck       = "confirm_check"
	StatusCheck              = "check"
	StatusFail               = "fail"
	StatusCancel             = "cancel"
	StatusSystemFail         = "system_fail"
	StatusRefundProcess      = "refund_process"
	StatusRefundFail         = "refund_fail"
	StatusRefundPaid         = "refund_paid"
	StatusLocked             = "locked"
)

// KnownStatus limits acknowledgements to documented payment lifecycle events.
func KnownStatus(status string) bool {
	switch status {
	case StatusPaid, StatusPaidOver, StatusWrongAmount, StatusWrongAmountWaiting,
		StatusProcess, StatusConfirmCheck, StatusCheck, StatusFail, StatusCancel,
		StatusSystemFail, StatusRefundProcess, StatusRefundFail, StatusRefundPaid, StatusLocked:
		return true
	default:
		return false
	}
}

// PaidStatus reports whether an invoice status represents a completed payment.
func PaidStatus(status string) bool {
	return status == StatusPaid || status == StatusPaidOver
}

type Config struct {
	MerchantID string
	APIKey     string
	// BaseURL overrides the production API endpoint; tests use it.
	BaseURL string
	// HTTPClient sends the API requests; nil selects a client with a
	// ten-second timeout.
	HTTPClient *http.Client
}

type Client struct {
	Config
	httpClient *http.Client
}

func NewClient(config Config) *Client {
	if config.BaseURL == "" {
		config.BaseURL = DefaultBaseURL
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{Config: config, httpClient: httpClient}
}

// Order describes the invoice to create. Amount is in minor units of Currency.
type Order struct {
	OrderNo   string
	Amount    int64
	Currency  string
	NotifyURL string
	ReturnURL string
	// Lifetime is the invoice validity in seconds; zero keeps the gateway
	// default (3600).
	Lifetime int64
}

// Invoice is the gateway's view of a payment. The info endpoint reports the
// settlement state in payment_status while the create endpoint uses status;
// Paid accepts either so callers do not depend on which endpoint produced it.
type Invoice struct {
	UUID          string `json:"uuid"`
	OrderNo       string `json:"order_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	PayerCurrency string `json:"payer_currency"`
	PaymentAmount string `json:"payment_amount"`
	URL           string `json:"url"`
	ExpiredAt     int64  `json:"expired_at"`
	Status        string `json:"status"`
	PaymentStatus string `json:"payment_status"`
	IsFinal       bool   `json:"is_final"`
}

// State returns the invoice's settlement state. The create endpoint reports
// it only in payment_status while the info endpoint fills both fields; the
// status field wins when both are present.
func (i *Invoice) State() string {
	if i.Status != "" {
		return i.Status
	}
	return i.PaymentStatus
}

func (i *Invoice) Paid() bool {
	return PaidStatus(i.State())
}

// Notification is the webhook payload for invoice payments.
type Notification struct {
	Type          string `json:"type"`
	UUID          string `json:"uuid"`
	OrderNo       string `json:"order_id"`
	Amount        string `json:"amount"`
	PaymentAmount string `json:"payment_amount"`
	Currency      string `json:"currency"`
	PayerCurrency string `json:"payer_currency"`
	Network       string `json:"network"`
	TxID          string `json:"txid"`
	IsFinal       bool   `json:"is_final"`
	Status        string `json:"status"`
	Sign          string `json:"sign"`
}

// APIError is a gateway-level failure: an HTTP error status or state != 0.
type APIError struct {
	HTTPStatus int
	State      int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cryptomus API error: http=%d state=%d message=%s", e.HTTPStatus, e.State, e.Message)
}

// IsNotFound recognizes only the gateway's explicit payment-not-found error.
// A proxy 404 or another missing resource (merchant, service, notification)
// does not establish that the invoice does not exist.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.State != 1 {
		return false
	}
	switch apiErr.HTTPStatus {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		message := strings.TrimSuffix(strings.TrimSpace(apiErr.Message), ".")
		return strings.EqualFold(message, "Payment not found")
	default:
		return false
	}
}

type invoiceRequest struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	OrderID     string `json:"order_id"`
	URLCallback string `json:"url_callback,omitempty"`
	URLReturn   string `json:"url_return,omitempty"`
	Lifetime    int64  `json:"lifetime,omitempty"`
}

type invoiceInfoRequest struct {
	UUID    string `json:"uuid,omitempty"`
	OrderID string `json:"order_id,omitempty"`
}

type apiResponse struct {
	State   int             `json:"state"`
	Result  json.RawMessage `json:"result"`
	Message string          `json:"message"`
	Errors  json.RawMessage `json:"errors"`
}

// CreateInvoice creates a hosted-checkout invoice for the order.
func (c *Client) CreateInvoice(ctx context.Context, order Order) (*Invoice, error) {
	if order.OrderNo == "" || order.Amount <= 0 || order.Currency == "" {
		return nil, errors.New("invalid Cryptomus order")
	}
	result, err := c.post(ctx, createInvoicePath, newInvoiceRequest(order))
	if err != nil {
		return nil, err
	}
	return decodeInvoice(result)
}

// newInvoiceRequest is the invoice creation request for order; the amount is
// sent exactly, as payment.FormatAmount renders it.
func newInvoiceRequest(order Order) invoiceRequest {
	return invoiceRequest{
		Amount:      payment.FormatAmount(order.Amount),
		Currency:    strings.ToUpper(order.Currency),
		OrderID:     order.OrderNo,
		URLCallback: order.NotifyURL,
		URLReturn:   order.ReturnURL,
		Lifetime:    order.Lifetime,
	}
}

// GetInvoice fetches the invoice by gateway UUID or, when uuid is empty, by
// the merchant order number.
func (c *Client) GetInvoice(ctx context.Context, uuid, orderNo string) (*Invoice, error) {
	if uuid == "" && orderNo == "" {
		return nil, errors.New("invoice lookup requires a uuid or order number")
	}
	request := invoiceInfoRequest{UUID: uuid}
	if uuid == "" {
		request.OrderID = orderNo
	}
	result, err := c.post(ctx, invoiceInfoPath, request)
	if err != nil {
		return nil, err
	}
	return decodeInvoice(result)
}

func decodeInvoice(result json.RawMessage) (*Invoice, error) {
	var invoice Invoice
	if err := json.Unmarshal(result, &invoice); err != nil {
		return nil, fmt.Errorf("decode Cryptomus invoice: %w", err)
	}
	if invoice.UUID == "" {
		return nil, errors.New("cryptomus invoice has no uuid")
	}
	return &invoice, nil
}

func (c *Client) post(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	if c.MerchantID == "" || c.APIKey == "" {
		return nil, errors.New("incomplete Cryptomus configuration")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Cryptomus request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Cryptomus request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("merchant", c.MerchantID)
	req.Header.Set("sign", c.signPayload(body))

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("cryptomus request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	value, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read Cryptomus response: %w", err)
	}
	if len(value) > maxResponseSize {
		return nil, errors.New("cryptomus response is too large")
	}
	var response apiResponse
	if err := json.Unmarshal(value, &response); err != nil {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Message: "non-JSON response"}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || response.State != 0 {
		message := response.Message
		if message == "" && len(response.Errors) > 0 {
			message = string(response.Errors)
		}
		return nil, &APIError{HTTPStatus: resp.StatusCode, State: response.State, Message: message}
	}
	if len(response.Result) == 0 {
		return nil, errors.New("cryptomus response has no result")
	}
	return response.Result, nil
}

// signPayload implements the documented request signature:
// md5(base64(json_body) + api_key), lowercase hex.
func (c *Client) signPayload(body []byte) string {
	return protocolkey.Md5Encode(base64.StdEncoding.EncodeToString(body)+c.APIKey, false)
}

// signMember matches the top-level sign field Cryptomus appends to webhook
// payloads. The value is a 32-character hex digest, so the pattern cannot
// match inside any other field's data.
var signMember = regexp.MustCompile(`,\s*"sign"\s*:\s*"[0-9a-fA-F]{32}"`)

// ParseNotification decodes a webhook payload without verifying it.
func ParseNotification(body []byte) (*Notification, error) {
	var notification Notification
	if err := json.Unmarshal(body, &notification); err != nil {
		return nil, fmt.Errorf("decode Cryptomus notification: %w", err)
	}
	return &notification, nil
}

// VerifyNotificationSign authenticates a webhook payload. Cryptomus signs the
// JSON body with the sign member removed: md5(base64(json) + api_key). The
// remaining bytes must stay untouched, so the sign member is stripped from
// the raw payload instead of re-encoding it.
func (c *Client) VerifyNotificationSign(body []byte) bool {
	if c.APIKey == "" {
		return false
	}
	notification, err := ParseNotification(body)
	if err != nil || len(notification.Sign) != 32 {
		return false
	}
	matches := signMember.FindAll(body, 2)
	if len(matches) != 1 {
		return false
	}
	unsigned := signMember.ReplaceAll(body, nil)
	expected := protocolkey.Md5Encode(base64.StdEncoding.EncodeToString(unsigned)+c.APIKey, false)
	received := strings.ToLower(notification.Sign)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(received)) == 1
}

// ParseMoney converts a gateway decimal amount to integer minor units. The
// gateway may report fiat amounts with up to eight decimal places; only
// trailing zeros beyond two decimals are tolerated, anything else cannot be
// represented in minor units and is rejected.
func ParseMoney(value string) (int64, error) {
	if whole, fraction, ok := strings.Cut(value, "."); ok && len(fraction) > 2 {
		trimmed := strings.TrimRight(fraction, "0")
		if len(trimmed) > 2 {
			return 0, errors.New("amount has more than two decimal places")
		}
		if trimmed == "" {
			value = whole
		} else {
			value = whole + "." + trimmed
		}
	}
	return payment.ParseAmount(value)
}
