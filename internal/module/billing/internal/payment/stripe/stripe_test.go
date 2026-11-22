package stripe

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/stripe/stripe-go/v81/webhook"
)

// Two Stripe payment methods serve checkouts concurrently. Each request must
// carry its own method's secret key: the process-wide key this replaced let
// one method's intents be created in another merchant account.
func TestConcurrentClientsUseTheirOwnSecretKey(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	backends := fake.Backends
	clients := map[string]*Client{
		"a": NewClient(Config{SecretKey: "sk_test_a", PublicKey: "pk_a", Backends: backends}),
		"b": NewClient(Config{SecretKey: "sk_test_b", PublicKey: "pk_b", Backends: backends}),
	}
	var wg sync.WaitGroup
	for merchant, client := range clients {
		for i := range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sheet, err := client.CreatePaymentSheet(context.Background(), &Order{
					OrderNo: fmt.Sprintf("%s-%d", merchant, i), Amount: 1990, Currency: "USD", Payment: "card",
				}, nil)
				if err != nil {
					t.Errorf("CreatePaymentSheet: %v", err)
					return
				}
				if sheet.PublishableKey != "pk_"+merchant {
					t.Errorf("sheet of %s carries publishable key %q", merchant, sheet.PublishableKey)
				}
			}()
		}
	}
	wg.Wait()
	keys := fake.Keys()
	if len(keys) != 20 {
		t.Fatalf("intents created = %d, want 20", len(keys))
	}
	for orderNo, key := range keys {
		if want := "sk_test_" + orderNo[:1]; key != want {
			t.Fatalf("intent for %s was created with key %q, want %q", orderNo, key, want)
		}
	}
}

func TestPaymentSheetLifecycle(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	client := NewClient(Config{SecretKey: "sk_test_a", PublicKey: "pk_a", Backends: fake.Backends})
	ctx := context.Background()
	order := &Order{OrderNo: "order-1", Amount: 1990, Currency: "USD", Payment: "card"}

	sheet, err := client.CreatePaymentSheet(ctx, order, &User{UserId: 7, Email: "buyer@example.test"})
	if err != nil {
		t.Fatalf("CreatePaymentSheet: %v", err)
	}
	if sheet.TradeNo != "pi_order-1" || sheet.ClientSecret != "pi_order-1_secret" || sheet.Customer != "cus_1" || sheet.EphemeralKey != "ek_test_secret" {
		t.Fatalf("sheet = %+v", sheet)
	}
	again, err := client.GetPaymentSheet(ctx, order, sheet.TradeNo)
	if err != nil || again.ClientSecret != sheet.ClientSecret {
		t.Fatalf("GetPaymentSheet = (%+v, %v)", again, err)
	}
	// The stored intent is checked against the order's payment expectation.
	for _, changed := range []Order{
		{OrderNo: "order-2", Amount: 1990, Currency: "USD", Payment: "card"},
		{OrderNo: "order-1", Amount: 1000, Currency: "USD", Payment: "card"},
		{OrderNo: "order-1", Amount: 1990, Currency: "EUR", Payment: "card"},
		{OrderNo: "order-1", Amount: 1990, Currency: "USD", Payment: "alipay"},
	} {
		if _, err := client.GetPaymentSheet(ctx, &changed, sheet.TradeNo); err == nil {
			t.Fatalf("intent matched a different order %+v", changed)
		}
		if _, err := client.VerifyPaymentIntent(ctx, &changed, sheet.TradeNo); err == nil {
			t.Fatalf("intent verified for a different order %+v", changed)
		}
	}
	if paid, err := client.VerifyPaymentIntent(ctx, order, sheet.TradeNo); err != nil || paid {
		t.Fatalf("unpaid intent verified as (%t, %v)", paid, err)
	}
	fake.SetStatus(sheet.TradeNo, "succeeded")
	if paid, err := client.VerifyPaymentIntent(ctx, order, sheet.TradeNo); err != nil || !paid {
		t.Fatalf("succeeded intent verified as (%t, %v)", paid, err)
	}
	if paid, err := client.QueryOrderStatus(ctx, sheet.TradeNo); err != nil || !paid {
		t.Fatalf("QueryOrderStatus = (%t, %v)", paid, err)
	}
	fake.SetStatus(sheet.TradeNo, "requires_payment_method")
	if err := client.CancelPaymentIntent(ctx, sheet.TradeNo); err != nil {
		t.Fatalf("CancelPaymentIntent: %v", err)
	}
	if canceled := fake.Canceled(); len(canceled) != 1 {
		t.Fatalf("canceled intents = %v", canceled)
	}
	if _, err := client.GetPaymentSheet(ctx, order, sheet.TradeNo); err == nil {
		t.Fatal("a canceled intent must not be offered again")
	}
	if err := client.CancelPaymentIntent(ctx, ""); err == nil {
		t.Fatal("canceling without an intent must fail")
	}
}

// A guest has no stable identity, so no customer is created or reused.
func TestGuestPaymentSheetHasNoCustomer(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	client := NewClient(Config{SecretKey: "sk_test_a", Backends: fake.Backends})
	sheet, err := client.CreatePaymentSheet(context.Background(), &Order{OrderNo: "guest-1", Amount: 500, Currency: "usd", Payment: "card"}, &User{})
	if err != nil {
		t.Fatalf("CreatePaymentSheet: %v", err)
	}
	if sheet.Customer != "" || sheet.EphemeralKey != "" || fake.Customers() != 0 {
		t.Fatalf("guest sheet = %+v, customers = %d", sheet, fake.Customers())
	}
	if _, err := client.CreatePaymentSheet(context.Background(), &Order{OrderNo: "", Amount: 500, Currency: "usd", Payment: "card"}, nil); err == nil {
		t.Fatal("an order without a number must be rejected")
	}
}

func TestWebhookEndpointLifecycle(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	client := NewClient(Config{SecretKey: "sk_test_a", Backends: fake.Backends})
	endpoint, err := client.CreateWebhookEndpoint(context.Background(), "https://panel.example.test/v1/notify/Stripe/token")
	if err != nil || endpoint.Secret == "" || fake.Webhooks()[endpoint.ID] == "" {
		t.Fatalf("CreateWebhookEndpoint = (%+v, %v)", endpoint, err)
	}
	if err := client.DeleteWebhookEndpoint(context.Background(), endpoint.ID); err != nil || len(fake.Webhooks()) != 0 {
		t.Fatalf("DeleteWebhookEndpoint = %v, endpoints %v", err, fake.Webhooks())
	}
}

func TestParseNotifyVerifiesTheWebhookSignature(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})
	payload := []byte(`{"id":"evt_1","object":"event","type":"payment_intent.succeeded","api_version":"2024-04-10","data":{"object":{"id":"pi_1","object":"payment_intent","amount_received":1990,"currency":"usd","metadata":{"order_no":"order-1","user_id":"7"},"payment_method_types":["card"]}}}`)
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: "whsec_test", Timestamp: time.Now()})
	notify, err := client.ParseNotify(signed.Payload, signed.Header)
	if err != nil {
		t.Fatalf("ParseNotify: %v", err)
	}
	if notify.EventType != "payment_intent.succeeded" || notify.OrderNo != "order-1" || notify.TradeNo != "pi_1" ||
		notify.Amount != 1990 || notify.Currency != "usd" || notify.Method != "card" || notify.UserId != 7 {
		t.Fatalf("notify = %+v", notify)
	}
	forged := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: "whsec_other", Timestamp: time.Now()})
	if _, err := client.ParseNotify(forged.Payload, forged.Header); err == nil {
		t.Fatal("a payload signed with another secret must be rejected")
	}
}
