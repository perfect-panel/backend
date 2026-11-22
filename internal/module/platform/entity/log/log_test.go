package log

import (
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

func TestSecurityLogMarshalKeepsRiskMetadataAndRedactsSecrets(t *testing.T) {
	tests := []struct {
		name         string
		marshal      func() ([]byte, error)
		secrets      []string
		riskMetadata []string
	}{
		{
			name: "balance",
			marshal: func() ([]byte, error) {
				return (&Balance{Metadata: requestmeta.Metadata{
					ClientIP: "192.0.2.13", UserAgent: "private-agent", ActorID: 7,
					IPMetadata: requestmeta.IPMetadata{IPCountryCode: "SG", IPASN: 64500, IPASOrganization: "Example Network"},
				}, Amount: 100}).Marshal()
			},
			riskMetadata: []string{"192.0.2.13", "private-agent", `"actor_id":7`, `"ip_country_code":"SG"`, `"ip_asn":64500`, `"ip_as_organization":"Example Network"`},
		},
		{
			name: "order",
			marshal: func() ([]byte, error) {
				return (&OrderCreated{Metadata: requestmeta.Metadata{
					ClientIP: "192.0.2.14", UserAgent: "order-agent", ActorID: 8,
				}, OrderNo: "202608290001", OrderType: 1, Amount: 9900, Method: "stripe", Source: "user"}).Marshal()
			},
			riskMetadata: []string{"192.0.2.14", "order-agent", `"actor_id":8`, "202608290001", `"amount":9900`, `"source":"user"`},
		},
		{
			name: "message",
			marshal: func() ([]byte, error) {
				return (&Message{
					To:       "person@example.com",
					Subject:  "Hello person@example.com",
					Content:  map[string]any{"code": "123456", "body": "private body"},
					Template: "private template",
					Status:   1,
				}).Marshal()
			},
			secrets: []string{"person@example.com", "123456", "private body", "private template"},
		},
		{
			name: "login",
			marshal: func() ([]byte, error) {
				return (&Login{IPMetadata: requestmeta.IPMetadata{IPCountry: "Singapore", IPCity: "Singapore"}, LoginIP: "192.0.2.10", UserAgent: "private-agent", Success: true}).Marshal()
			},
			riskMetadata: []string{"192.0.2.10", "private-agent", `"ip_country":"Singapore"`, `"ip_city":"Singapore"`},
		},
		{
			name: "registration",
			marshal: func() ([]byte, error) {
				return (&Register{Identifier: "person@example.com", RegisterIP: "192.0.2.11", UserAgent: "private-agent"}).Marshal()
			},
			secrets:      []string{"person@example.com"},
			riskMetadata: []string{"192.0.2.11", "private-agent"},
		},
		{
			name: "subscription",
			marshal: func() ([]byte, error) {
				return (&Subscribe{Token: "bearer-token", ClientIP: "192.0.2.12", UserAgent: "private-agent"}).Marshal()
			},
			secrets:      []string{"bearer-token"},
			riskMetadata: []string{"192.0.2.12", "private-agent"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.marshal()
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			for _, secret := range tc.secrets {
				if strings.Contains(text, secret) {
					t.Fatalf("serialized audit log contains %q: %s", secret, text)
				}
			}
			for _, value := range tc.riskMetadata {
				if !strings.Contains(text, value) {
					t.Fatalf("serialized audit log lost risk metadata %q: %s", value, text)
				}
			}
			if len(tc.secrets) > 0 && !strings.Contains(text, logger.RedactedValue) {
				t.Fatalf("serialized audit log is not marked redacted: %s", text)
			}
		})
	}
}

func TestExpirableTypesNeverIncludesFinancialLedgers(t *testing.T) {
	financial := map[int]bool{
		int(TypeBalance):      true,
		int(TypeCommission):   true,
		int(TypeGift):         true,
		int(TypeOrderCreated): true,
	}
	for _, typ := range ExpirableTypes() {
		if financial[typ] {
			t.Fatalf("financial log type %d must not be expirable", typ)
		}
	}
}

func TestSecurityLogUnmarshalRetainsRiskMetadataAndRedactsSecrets(t *testing.T) {
	var message Message
	if err := message.Unmarshal([]byte(`{"to":"person@example.com","subject":"Hello","content":{"code":"123456"},"template":"secret","platform":"smtp","status":1}`)); err != nil {
		t.Fatal(err)
	}
	if message.To != logger.RedactedValue || message.Subject != logger.RedactedValue || message.Template != logger.RedactedValue || message.Content["redacted"] != true || message.Platform != "smtp" || message.Status != 1 {
		t.Fatalf("legacy message audit was not safely decoded: %+v", message)
	}

	var subscribe Subscribe
	if err := subscribe.Unmarshal([]byte(`{"token":"legacy-token","user_agent":"legacy-agent","client_ip":"192.0.2.1","user_subscribe_id":7}`)); err != nil {
		t.Fatal(err)
	}
	if subscribe.Token != logger.RedactedValue || subscribe.UserAgent != "legacy-agent" || subscribe.ClientIP != "192.0.2.1" || subscribe.UserSubscribeId != 7 {
		t.Fatalf("legacy subscription audit was not safely decoded: %+v", subscribe)
	}

	var login Login
	if err := login.Unmarshal([]byte(`{"method":"email","login_ip":"192.0.2.2","user_agent":"legacy-agent","success":true}`)); err != nil {
		t.Fatal(err)
	}
	if login.LoginIP != "192.0.2.2" || login.UserAgent != "legacy-agent" || !login.Success {
		t.Fatalf("legacy login audit was not safely decoded: %+v", login)
	}
}

func TestRiskMetadataIsBoundedWithoutBreakingUTF8(t *testing.T) {
	ua := strings.Repeat("客", 300)
	data, err := (&Login{LoginIP: strings.Repeat("1", 300), UserAgent: ua}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Login
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if len(decoded.LoginIP) != 255 || len(decoded.UserAgent) > 512 || !strings.HasPrefix(ua, decoded.UserAgent) {
		t.Fatalf("risk metadata bounds were not enforced: ip=%d ua=%d", len(decoded.LoginIP), len(decoded.UserAgent))
	}
}
