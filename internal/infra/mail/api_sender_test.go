package mail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	stdmail "net/mail"
	"reflect"
	"strings"
	"testing"
)

const (
	resendConfig     = `{"api_key":"test-key","from":"sender@example.com","reply_to":"support@example.com"}`
	cloudflareConfig = `{"account_id":"abc123","api_token":"test-token","from":"sender@example.com"}`
)

func TestEmailAPIRequests(t *testing.T) {
	for _, tc := range []struct {
		name, config, endpoint, token, receipt string
	}{
		{"resend", resendConfig, "https://api.resend.com/emails", "test-key", `{"id":"message-id"}`},
		{"cloudflare", cloudflareConfig, "https://api.cloudflare.com/client/v4/accounts/abc123/email/sending/send", "test-token",
			`{"success":true,"result":{"message_id":"message-id","delivered":["a@example.com"],"queued":["b@example.com"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			to := []string{"a@example.com", "b@example.com"}
			got, err := NewSender(tc.name, tc.config, "测试站点")
			if err != nil {
				t.Fatal(err)
			}
			sender := got.(*apiSender)
			if sender.endpoint != tc.endpoint || sender.client.Timeout != sendTimeout {
				t.Fatal("unexpected API endpoint or timeout")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+tc.token || r.Header.Get("Content-Type") != "application/json" {
					t.Error("unexpected request method or headers")
				}
				var msg struct {
					From          json.RawMessage
					To            []string
					Subject, HTML string
					ReplyTo       *string `json:"reply_to"`
				}
				if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(to, msg.To) || msg.Subject != "验证码" || msg.HTML != "<p>123</p>" {
					t.Error("email content changed")
				}
				checkAPIAddresses(t, tc.name, msg.From, msg.ReplyTo)
				_, _ = w.Write([]byte(tc.receipt))
			}))
			t.Cleanup(server.Close)
			sender.endpoint = server.URL
			if err := sender.SendContext(t.Context(), to, "验证码", "<p>123</p>"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func checkAPIAddresses(t *testing.T, provider string, from json.RawMessage, replyTo *string) {
	t.Helper()
	if provider == "resend" {
		var raw string
		if err := json.Unmarshal(from, &raw); err != nil {
			t.Error(err)
			return
		}
		address, err := stdmail.ParseAddress(raw)
		if err != nil || address.Address != "sender@example.com" || address.Name != "测试站点" || replyTo == nil || *replyTo != "support@example.com" {
			t.Error("unexpected Resend addresses")
		}
	} else {
		var address map[string]string
		if err := json.Unmarshal(from, &address); err != nil || address["address"] != "sender@example.com" || address["name"] != "测试站点" || replyTo != nil {
			t.Error("unexpected Cloudflare addresses")
		}
	}
}

func TestEmailAPIFailures(t *testing.T) {
	for _, tc := range []struct {
		provider Platform
		status   int
		receipt  string
		want     string
	}{
		{Resend, 401, `{"message":"test-secret recipient@example.com"}`, "HTTP 401"},
		{Resend, 429, "", "HTTP 429"},
		{Resend, 200, `{"message":"test-secret"}`, "message ID"},
		{Cloudflare, 200, `{"success":false,"errors":[{"message":"test-secret"}]}`, "unsuccessful"},
		{Cloudflare, 200, `{"success":true,"result":{"message_id":"id","permanent_bounces":["recipient@example.com"]}}`, "bounced: 1"},
		{Cloudflare, 200, `{"success":true,"result":{"message_id":"id","suppressed_recipients":["recipient@example.com"]}}`, "suppressed: 1"},
		{Cloudflare, 200, `{"success":true,"result":{"message_id":"id"}}`, "incomplete"},
		{Cloudflare, 200, "<html>", "invalid"},
		{Resend, 200, strings.Repeat("x", 1<<17), "could not read"},
	} {
		t.Run(tc.provider.String()+"/"+tc.want, func(t *testing.T) {
			cfg := resendConfig
			if tc.provider == Cloudflare {
				cfg = cloudflareConfig
			}
			client, err := NewSender(tc.provider.String(), cfg, "")
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.receipt))
			}))
			t.Cleanup(server.Close)
			sender := client.(*apiSender)
			sender.endpoint = server.URL
			err = sender.SendContext(t.Context(), []string{"recipient@example.com"}, "subject", "body")
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "recipient@example.com") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEmailAPIRedirectAndCancellation(t *testing.T) {
	client, err := NewSender("resend", resendConfig, "")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Error("followed email API redirect")
		}
		http.Redirect(w, r, "/next", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	sender := client.(*apiSender)
	sender.endpoint = server.URL
	recipients := []string{"to@example.com"}
	if err := sender.SendContext(t.Context(), recipients, "s", "h"); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sender.SendContext(ctx, recipients, "s", "h"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error: %v", err)
	}
}

func TestEmailAPIInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ provider, config string }{
		{"resend", `{"api_key":"sensitive-value","from":"invalid"}`},
		{"resend", `{"api_key":"sensitive-value","from":"a@example.com","reply_to":"invalid"}`},
		{"cloudflare", `{"api_token":"sensitive-value","from":"a@example.com","account_id":"../other"}`},
		{"cloudflare", `{"api_token":"sensitive-value"`},
	} {
		sender, err := NewSender(tc.provider, tc.config, "")
		if sender != nil || err == nil || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatalf("%s: expected a sanitized configuration error", tc.provider)
		}
	}
}
