package paypal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
)

// ErrOrderNotFound is a merchant-authenticated RESOURCE_NOT_FOUND response.
var ErrOrderNotFound = errors.New("PayPal order resource not found")

var orderID = regexp.MustCompile(`^[A-Za-z0-9]{1,36}$`)

type Money struct {
	Currency string `json:"currency_code"`
	Value    string `json:"value"`
}

func (m Money) Matches(amount int64, currency string) bool {
	minor, err := payment.ParseAmount(m.Value)
	return err == nil && minor == amount && minor > 0 && m.Currency == currency && currency != ""
}

type Capture struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount Money  `json:"amount"`
}

type PurchaseUnit struct {
	Reference string `json:"reference_id"`
	Invoice   string `json:"invoice_id"`
	Amount    Money  `json:"amount"`
	Payments  struct {
		Captures []Capture         `json:"captures"`
		Refunds  []json.RawMessage `json:"refunds"`
	} `json:"payments"`
}

type Order struct {
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Intent string         `json:"intent"`
	Units  []PurchaseUnit `json:"purchase_units"`
	Links  []struct {
		Rel  string `json:"rel"`
		Href string `json:"href"`
	} `json:"links"`
}

func (o *Order) Unit() (*PurchaseUnit, error) {
	if !orderID.MatchString(o.ID) || o.Intent != "CAPTURE" || len(o.Units) != 1 {
		return nil, errors.New("invalid PayPal order identity or intent")
	}
	u := &o.Units[0]
	if u.Reference == "" || u.Reference != u.Invoice {
		return nil, errors.New("PayPal order reference mismatch")
	}
	return u, nil
}

func (o *Order) Matches(orderNo string, amount int64, currency string) bool {
	unit, err := o.Unit()
	return err == nil && unit.Reference == orderNo && unit.Amount.Matches(amount, currency)
}

// This gateway creates one purchase unit and captures it in full exactly once.
func (o *Order) Paid(amount int64, currency, captureID string) bool {
	unit, err := o.Unit()
	if err != nil || o.Status != "COMPLETED" || len(unit.Payments.Captures) != 1 || len(unit.Payments.Refunds) != 0 {
		return false
	}
	c := unit.Payments.Captures[0]
	return c.ID != "" && c.Status == "COMPLETED" && (captureID == "" || c.ID == captureID) && c.Amount.Matches(amount, currency)
}

func ApprovalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.paypal.com", "www.sandbox.paypal.com", "paypal.com", "sandbox.paypal.com":
		return true
	default:
		return false
	}
}

func (o *Order) ApprovalLink() string {
	for _, l := range o.Links {
		if (l.Rel == "payer-action" || l.Rel == "approve") && ApprovalURL(l.Href) {
			return l.Href
		}
	}
	return ""
}

func key(operation, id string) string {
	sum := sha256.Sum256([]byte(operation + ":" + id))
	return hex.EncodeToString(sum[:16])
}

func (c *Client) CreateBody(orderNo string, amount int64, currency, returnURL string) ([]byte, error) {
	u, err := url.Parse(returnURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("PayPal checkout requires an absolute return URL")
	}
	if amount <= 0 || orderNo == "" || len(orderNo) > 127 || len(currency) != 3 {
		return nil, errors.New("invalid PayPal charge or order number")
	}
	value := payment.FormatAmount(amount)
	switch currency {
	case "HUF", "JPY", "TWD":
		if amount%100 != 0 {
			return nil, errors.New("PayPal requires a whole amount in this currency")
		}
		value = strconv.FormatInt(amount/100, 10)
	}
	experience := map[string]string{
		"user_action": "PAY_NOW", "shipping_preference": "NO_SHIPPING",
		"return_url": returnURL, "cancel_url": returnURL,
	}
	if c.locale != "" {
		experience["locale"] = c.locale
	}
	return json.Marshal(map[string]any{
		"intent":         "CAPTURE",
		"purchase_units": []map[string]any{{"reference_id": orderNo, "invoice_id": orderNo, "amount": Money{Currency: currency, Value: value}}},
		"payment_source": map[string]any{"paypal": map[string]any{"experience_context": experience}},
	})
}

func (c *Client) Create(ctx context.Context, no string, body []byte) (*Order, error) {
	var o Order
	err := c.request(ctx, http.MethodPost, "/v2/checkout/orders", key("create", no), body, &o)
	if err != nil {
		return nil, err
	}
	// Some approval responses contain only an ID, status and HATEOAS links.
	// Fetch the full purchase unit before handing its approval link to a buyer.
	if len(o.Units) == 0 {
		full, err := c.Get(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		if full.ApprovalLink() == "" {
			full.Links = o.Links
		}
		return full, nil
	}
	return &o, nil
}

func (c *Client) Get(ctx context.Context, id string) (*Order, error) {
	if !orderID.MatchString(id) {
		return nil, errors.New("invalid PayPal order ID")
	}
	var o Order
	if err := c.request(ctx, http.MethodGet, "/v2/checkout/orders/"+id, "", nil, &o); err != nil {
		return nil, err
	}
	if o.ID != id {
		return nil, errors.New("PayPal returned another order")
	}
	return &o, nil
}

func (c *Client) Capture(ctx context.Context, id string) error {
	if !orderID.MatchString(id) {
		return errors.New("invalid PayPal order ID")
	}
	var result Order
	return c.request(ctx, http.MethodPost, "/v2/checkout/orders/"+id+"/capture", key("capture", id), []byte("{}"), &result)
}
