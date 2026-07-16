package stripe

import (
	"encoding/json"
	"fmt"
	"strconv"

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
	stripe.Key = c.SecretKey
	// Create a new Stripe customer if it does not exist
	customerDataRes, err := c.SearchStripeCustomer(user)
	if err != nil {
		return nil, err
	}
	if customerDataRes == nil {
		customerDataRes, err = c.CreateCustomer(user)
		if err != nil {
			return nil, err
		}
	}
	// Create Ephemeral Key
	ekParams := &stripe.EphemeralKeyParams{
		Customer:      stripe.String(customerDataRes.ID),
		StripeVersion: stripe.String(APIVersion),
	}
	ek, err := ephemeralkey.New(ekParams)
	if err != nil {
		return nil, err
	}
	// Create Payment Intent
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(order.Amount),
		Customer: stripe.String(customerDataRes.ID),
		Currency: stripe.String(order.Currency),
		PaymentMethodTypes: []*string{
			stripe.String(order.Payment),
		},
		Metadata: map[string]string{
			"order_no":  order.OrderNo,
			"user_id":   strconv.FormatInt(user.UserId, 10),
			"subscribe": order.Subscribe,
		},
	}
	result, err := paymentintent.New(params)
	if err != nil {
		return nil, err
	}
	return &PaymentSheet{
		ClientSecret:   result.ClientSecret,
		EphemeralKey:   ek.Secret,
		Customer:       customerDataRes.ID,
		PublishableKey: c.PublicKey,
		TradeNo:        result.ID,
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
