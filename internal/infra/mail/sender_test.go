package mail

import (
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

func TestSMTPSenderAssemblyPreservesConfiguration(t *testing.T) {
	sender, err := NewSender("smtp", `{"host":"smtp.example.test","port":587,"user":"test","pass":"test-only","from":"mail@example.test","reply_to":"reply@example.test"}`, "Example")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := sender.(*SMTPClient)
	if !ok || client.conf.Host != "smtp.example.test" || client.conf.Port != 587 || client.conf.SiteName != "Example" || client.conf.ReplyTo != "reply@example.test" {
		t.Fatalf("unexpected SMTP sender: %+v", sender)
	}
	if client.timeout != sendTimeout {
		t.Fatalf("timeout = %s, want %s", client.timeout, sendTimeout)
	}
	if NewSMTPClient(nil) != nil {
		t.Fatal("nil configuration no longer returns nil")
	}
	if _, err := NewSender("unsupported", `{}`, "Example"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
}

// A configuration that fails to parse is reported without its content: it
// holds the relay password.
func TestNewSenderDoesNotLogConfiguration(t *testing.T) {
	collector := logtest.NewCollector(t)
	_, err := NewSender("smtp", `{"pass":"sentinel-password",`, "Example")
	if err == nil {
		t.Fatal("a broken configuration was accepted")
	}
	if strings.Contains(err.Error(), "sentinel-password") || strings.Contains(collector.String(), "sentinel-password") {
		t.Fatalf("the password leaked: error %q, log %q", err, collector.String())
	}
}

func TestSendersReuseTheSenderPerConfiguration(t *testing.T) {
	var senders Senders
	config := `{"host":"smtp.example.test","port":587}`
	first, err := senders.Get("smtp", config, "Example")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again, _ := senders.Get("smtp", config, "Example"); again != first {
		t.Fatal("the same configuration built a second sender")
	}
	renamed, _ := senders.Get("smtp", config, "Renamed")
	if renamed == first || renamed.(*SMTPClient).conf.SiteName != "Renamed" {
		t.Fatal("a new site name kept the old sender")
	}
}

func TestMailPlatformNames(t *testing.T) {
	if parsePlatform(SMTP.String()) != SMTP || parsePlatform("x") != unsupported || unsupported.String() != "unsupported" {
		t.Fatal("platform names do not round-trip")
	}
}
