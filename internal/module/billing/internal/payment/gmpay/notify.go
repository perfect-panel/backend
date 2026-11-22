package gmpay

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func signature(fields map[string]string, secret string) string {
	pairs := make([]string, 0, len(fields))
	for k, v := range fields {
		if k != "signature" && v != "" {
			pairs = append(pairs, k+"="+v)
		}
	}
	sort.Strings(pairs)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(pairs, "&")))
	return hex.EncodeToString(mac.Sum(nil))
}

type Notification struct {
	OrderID, TradeID string
	Amount           int64
}

// The signed object is flat. Decode its fields once so that duplicate names,
// JSON types and the exact decimal spelling of the fiat amount remain visible.
func notificationFields(body []byte) (map[string]any, error) {
	if !utf8.Valid(body) || !json.Valid(body) {
		return nil, errors.New("invalid GMPay JSON")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if start, err := d.Token(); err != nil || start != json.Delim('{') {
		return nil, errors.New("expected GMPay JSON object")
	}
	fields := make(map[string]any)
	for d.More() {
		name, err := d.Token()
		if err != nil {
			return nil, err
		}
		key := name.(string)
		if _, exists := fields[key]; exists || key == "" || strings.ContainsAny(key, "=&") {
			return nil, errors.New("ambiguous GMPay notification field")
		}
		value, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch value.(type) {
		case nil, string, json.Number:
			fields[key] = value
		default:
			return nil, errors.New("unexpected GMPay notification value")
		}
	}
	return fields, nil
}

func (c *Client) VerifyNotification(body []byte) (*Notification, error) {
	fields, err := notificationFields(body)
	if err != nil {
		return nil, err
	}
	canonical := make(map[string]string, len(fields))
	for k, v := range fields {
		switch value := v.(type) {
		case string:
			// GMPay joins raw fields with '&' without escaping separators.
			if strings.Contains(value, "&") {
				return nil, errors.New("ambiguous GMPay notification value")
			}
			canonical[k] = value
		case json.Number:
			f, err := value.Float64()
			if err != nil {
				return nil, err
			}
			canonical[k] = strconv.FormatFloat(f, 'f', -1, 64)
		}
	}
	sig, _ := fields["signature"].(string)
	if fields["pid"] != c.config.PID || !hmac.Equal([]byte(sig), []byte(signature(canonical, c.config.Secret))) {
		return nil, errors.New("GMPay notification authentication failed")
	}
	tradeID, _ := fields["trade_id"].(string)
	orderID, _ := fields["order_id"].(string)
	if fields["status"] != json.Number("2") || orderID == "" || len(orderID) > 32 || !tradeIDPattern.MatchString(tradeID) {
		return nil, errors.New("invalid GMPay paid notification")
	}
	// The upstream HMAC normalizes numbers using float64. For settlement the
	// original decimal and its signed spelling must agree exactly in cents.
	rawAmount, _ := fields["amount"].(json.Number)
	amount, err := parseFiat(rawAmount.String())
	signedAmount, signedErr := parseFiat(canonical["amount"])
	if err != nil || signedErr != nil || amount <= 0 || amount != signedAmount {
		return nil, errors.New("invalid signed GMPay fiat amount")
	}
	return &Notification{OrderID: orderID, TradeID: tradeID, Amount: amount}, nil
}
