package stripe

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/stripe/stripe-go/v81/webhookendpoint"

	"github.com/perfect-panel/server/pkg/logger"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/ephemeralkey"
	"github.com/stripe/stripe-go/v81/paymentintent"
	"github.com/stripe/stripe-go/v81/paymentmethod"
	"github.com/stripe/stripe-go/v81/webhook"
)

const APIVersion = "2024-04-10"

type Config struct {
	PublicKey     string
	SecretKey     string
	WebhookSecret string
}

type User struct {
	UserId int64
	Email  string
}
type NotifyResult struct {
	EventType string
	OrderNo   string
	TradeNo   string
	Method    string
	UserId    int64
	Amount    int64
	Currency  string
}
type Order struct {
	OrderNo   string
	Subscribe string
	Amount    int64
	Currency  string
	Payment   string
}

type Client struct {
	Config
}

type PaymentSheet struct {
	ClientSecret   string
	EphemeralKey   string
	Customer       string
	PublishableKey string
	TradeNo        string
}

func NewClient(config Config) *Client {
	return &Client{
		Config: config,
	}
}

func (c *Client) CreatePaymentSheet(order *Order, user *User) (*PaymentSheet, error) {
	if order == nil || order.OrderNo == "" || order.Amount < 0 || order.Currency == "" || order.Payment == "" {
		return nil, errors.New("invalid Stripe order")
	}
	stripe.Key = c.SecretKey
	var customerDataRes *stripe.Customer
	var err error
	var userID int64
	if user != nil {
		userID = user.UserId
	}
	// A guest checkout has no stable Stripe customer identity.  Do not reuse
	// the synthetic user_id=0 customer across unrelated buyers.
	if user != nil && (user.Email != "" || user.UserId != 0) {
		customerDataRes, err = c.SearchStripeCustomer(user)
		if err != nil {
			return nil, err
		}
		if customerDataRes == nil {
			customerDataRes, err = c.CreateCustomer(user)
			if err != nil {
				return nil, err
			}
		}
	}
	// Create Payment Intent
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(order.Amount),
		Currency: stripe.String(order.Currency),
		PaymentMethodTypes: []*string{
			stripe.String(order.Payment),
		},
		Metadata: map[string]string{
			"order_no":  order.OrderNo,
			"user_id":   strconv.FormatInt(userID, 10),
			"subscribe": order.Subscribe,
		},
	}
	if customerDataRes != nil {
		params.Customer = stripe.String(customerDataRes.ID)
	}
	// Retrying the checkout after a network timeout must return the same
	// PaymentIntent rather than creating another chargeable transaction.
	params.SetIdempotencyKey("ppanel:payment-intent:" + order.OrderNo)
	result, err := paymentintent.New(params)
	if err != nil {
		return nil, err
	}
	sheet := &PaymentSheet{
		ClientSecret:   result.ClientSecret,
		PublishableKey: c.PublicKey,
		TradeNo:        result.ID,
	}
	if customerDataRes != nil {
		// Preserve the original mobile-SDK support for identified users.  Guest
		// checkouts intentionally have no customer or ephemeral key.
		ekParams := &stripe.EphemeralKeyParams{
			Customer:      stripe.String(customerDataRes.ID),
			StripeVersion: stripe.String(APIVersion),
		}
		ek, err := ephemeralkey.New(ekParams)
		if err != nil {
			return nil, err
		}
		sheet.EphemeralKey = ek.Secret
		sheet.Customer = customerDataRes.ID
	}
	return sheet, nil
}

// GetPaymentSheet returns the already-created PaymentIntent for a repeat
// checkout.  It validates the immutable fields recorded in Stripe before
// exposing its client secret again.
func (c *Client) GetPaymentSheet(order *Order, tradeNo string) (*PaymentSheet, error) {
	if order == nil || tradeNo == "" {
		return nil, errors.New("invalid Stripe payment intent lookup")
	}
	stripe.Key = c.SecretKey
	intent, err := paymentintent.Get(tradeNo, nil)
	if err != nil {
		return nil, err
	}
	if intent.Metadata["order_no"] != order.OrderNo || intent.Amount != order.Amount ||
		!strings.EqualFold(string(intent.Currency), order.Currency) ||
		len(intent.PaymentMethodTypes) != 1 || intent.PaymentMethodTypes[0] != order.Payment {
		return nil, errors.New("stored Stripe payment intent does not match order")
	}
	if intent.Status == stripe.PaymentIntentStatusCanceled {
		return nil, errors.New("stored Stripe payment intent is canceled")
	}
	return &PaymentSheet{
		ClientSecret:   intent.ClientSecret,
		PublishableKey: c.PublicKey,
		TradeNo:        intent.ID,
	}, nil
}

// SearchStripeCustomer  Search for a Stripe customer by email or user ID
func (c *Client) SearchStripeCustomer(user *User) (*stripe.Customer, error) {
	stripe.Key = c.SecretKey
	params := &stripe.CustomerSearchParams{}
	if user.Email != "" {
		params.SearchParams.Query = fmt.Sprintf("email:'%s'", user.Email)
	} else {
		params.SearchParams.Query = fmt.Sprintf("metadata['user_id']:'%d'", user.UserId)
	}
	result := customer.Search(params)
	if result.Err() != nil {
		fmt.Printf("Error: %v\n", result.Err().Error())
		return nil, result.Err()
	}

	if len(result.CustomerSearchResult().Data) != 0 {
		return result.CustomerSearchResult().Data[0], nil
	}
	return nil, nil
}

// CreateCustomer Create a new Stripe customer
func (c *Client) CreateCustomer(user *User) (*stripe.Customer, error) {
	stripe.Key = c.SecretKey
	customerData := &stripe.CustomerParams{}
	if user.Email != "" {
		customerData.Email = &user.Email
	}
	customerData.AddMetadata("user_id", strconv.FormatInt(user.UserId, 10))
	return customer.New(customerData)
}

// QueryOrderStatus Query the status of the order
func (c *Client) QueryOrderStatus(orderNo string) (bool, error) {
	stripe.Key = c.SecretKey
	intent, err := paymentintent.Get(orderNo, nil)
	if err != nil {
		return false, err
	}
	return intent.Status == "succeeded", err
}

// VerifyPaymentIntent checks that the stored intent still belongs to the
// order before returning its payment state.  It is used by expiry handling so
// a successful intent can be settled instead of being closed locally.
func (c *Client) VerifyPaymentIntent(order *Order, tradeNo string) (bool, error) {
	if _, err := c.GetPaymentSheet(order, tradeNo); err != nil {
		return false, err
	}
	return c.QueryOrderStatus(tradeNo)
}

// CancelPaymentIntent prevents a still-pending client secret from being paid
// after the local order has expired.
func (c *Client) CancelPaymentIntent(tradeNo string) error {
	if tradeNo == "" {
		return errors.New("Stripe payment intent is missing")
	}
	stripe.Key = c.SecretKey
	_, err := paymentintent.Cancel(tradeNo, nil)
	return err
}

// ParseNotify
func (c *Client) ParseNotify(payload []byte, signature string) (*NotifyResult, error) {
	event, err := webhook.ConstructEventWithOptions(payload, signature, c.Config.WebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		return nil, err
	}
	var paymentIntent stripe.PaymentIntent
	err = json.Unmarshal(event.Data.Raw, &paymentIntent)
	if err != nil {
		logger.Error("Failed to unmarshal payment intent", logger.Field("error", err.Error()))
		return nil, err
	}
	orderNo := paymentIntent.Metadata["order_no"]
	userId := paymentIntent.Metadata["user_id"]
	var method string
	if len(paymentIntent.PaymentMethodTypes) > 0 {
		method = paymentIntent.PaymentMethodTypes[0]
	}
	// userId string 转 int64
	uid, _ := strconv.ParseInt(userId, 10, 64)
	return &NotifyResult{
		EventType: string(event.Type),
		OrderNo:   orderNo,
		TradeNo:   paymentIntent.ID,
		UserId:    uid,
		Method:    method,
		Amount:    paymentIntent.AmountReceived,
		Currency:  string(paymentIntent.Currency),
	}, nil
}

// RetrievePaymentMethod 查询支付方式
func (c *Client) RetrievePaymentMethod(id string) (*stripe.PaymentMethod, error) {
	stripe.Key = c.SecretKey
	return paymentmethod.Get(id, nil)
}

// CreateWebhookEndpoint 创建 webhook endpoint
func (c *Client) CreateWebhookEndpoint(url string) (*stripe.WebhookEndpoint, error) {
	stripe.Key = c.SecretKey
	params := &stripe.WebhookEndpointParams{
		URL: stripe.String(url),
		EnabledEvents: []*string{
			stripe.String("payment_intent.succeeded"),
			stripe.String("payment_intent.payment_failed"),
		},
	}
	return webhookendpoint.New(params)
}
