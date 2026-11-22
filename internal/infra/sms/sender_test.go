package sms

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// Building a sender runs whenever the provider configuration changes; the
// config it receives holds access keys, auth tokens and passwords, none of
// which may reach any log sink.
func TestNewSenderDoesNotLogProviderCredentials(t *testing.T) {
	const secret = "sentinel-provider-secret"
	var stdlog bytes.Buffer
	log.SetOutput(&stdlog)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	collector := logtest.NewCollector(t)

	for _, platform := range []string{AlibabaCloud.String(), Smsbao.String(), Abosend.String(), Twilio.String()} {
		config := fmt.Sprintf(`{"access":"access-id","secret":%q,"endpoint":"dysmsapi.aliyuncs.com","template":"{{.code}}"}`, secret)
		if _, err := NewSender(platform, config); err != nil {
			t.Fatalf("NewSender(%s): %v", platform, err)
		}
	}

	if strings.Contains(stdlog.String(), secret) || strings.Contains(collector.String(), secret) {
		t.Fatalf("provider secret leaked into logs:\nlog: %s\nlogger: %s", stdlog.String(), collector.String())
	}
}

type sentText struct{ area, mobile, text string }

type fakeTextProvider struct{ sent []sentText }

func (p *fakeTextProvider) SendText(_ context.Context, area, mobile, text string) error {
	p.sent = append(p.sent, sentText{area, mobile, text})
	return nil
}

type fakeTemplateProvider struct{ params []map[string]string }

func (p *fakeTemplateProvider) SendTemplate(_ context.Context, _, _ string, params map[string]string) error {
	p.params = append(p.params, params)
	return nil
}

func TestSendRendersTheTextTemplate(t *testing.T) {
	provider := &fakeTextProvider{}
	s := &sender{template: "Your code is {{.code}}, valid for {{.minutes}} minutes", text: provider}

	msg := Message{Area: "86", Mobile: "13800000000", Params: map[string]string{"code": "123456", "minutes": "5"}}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := s.Send(context.Background(), CodeMessage("1", "5550001111", "654321")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	want := []sentText{
		{"86", "13800000000", "Your code is 123456, valid for 5 minutes"},
		{"1", "5550001111", "Your code is 654321, valid for <no value> minutes"},
	}
	if len(provider.sent) != 2 || provider.sent[0] != want[0] || provider.sent[1] != want[1] {
		t.Fatalf("sent = %+v, want %+v", provider.sent, want)
	}
}

// A template that fails to render stops the send: an empty or half-rendered
// text would reach the user without the code.
func TestSendRefusesATemplateThatFailsToRender(t *testing.T) {
	for _, template := range []string{"Your code is {{.code", "{{template \"missing\"}}"} {
		provider := &fakeTextProvider{}
		err := (&sender{template: template, text: provider}).Send(context.Background(), CodeMessage("86", "13800000000", "123456"))
		if err == nil || !strings.Contains(err.Error(), "render sms template") {
			t.Fatalf("template %q: error = %v, want a render error", template, err)
		}
		if len(provider.sent) != 0 {
			t.Fatalf("template %q: sent %+v", template, provider.sent)
		}
	}
}

// Alibaba Cloud fills its own approved template: the parameters go through
// unrendered.
func TestSendPassesParametersToTemplateProviders(t *testing.T) {
	provider := &fakeTemplateProvider{}
	if err := (&sender{provided: provider}).Send(context.Background(), CodeMessage("86", "13800000000", "123456")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(provider.params) != 1 || provider.params[0]["code"] != "123456" {
		t.Fatalf("params = %v", provider.params)
	}
}

func TestSendHonoursACancelledContext(t *testing.T) {
	provider := &fakeTextProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&sender{template: "{{.code}}", text: provider}).Send(ctx, CodeMessage("86", "1", "2")); err == nil || len(provider.sent) != 0 {
		t.Fatalf("error = %v, sent = %+v, want the send abandoned", err, provider.sent)
	}
}

func TestNewSenderRejectsBadConfiguration(t *testing.T) {
	for platform, config := range map[string]string{
		"smsbao":  "{",
		"unknown": "{}",
	} {
		if _, err := NewSender(platform, config); err == nil {
			t.Fatalf("NewSender(%s, %s) accepted", platform, config)
		}
	}
}

// One sender serves every message sent under the same configuration; a new
// configuration gets a new sender.
func TestSendersReuseTheSenderPerConfiguration(t *testing.T) {
	var senders Senders
	config := `{"access":"u","secret":"p","template":"{{.code}}"}`
	first, err := senders.Get("smsbao", config)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	again, _ := senders.Get("smsbao", config)
	if first != again {
		t.Fatal("the same configuration built a second sender")
	}
	changed, _ := senders.Get("smsbao", `{"access":"u","secret":"other","template":"{{.code}}"}`)
	if changed == first {
		t.Fatal("a changed configuration kept the old sender")
	}
	if _, err := senders.Get("smsbao", "{"); err == nil {
		t.Fatal("a broken configuration built a sender")
	}
}

func TestPlatformNames(t *testing.T) {
	for _, p := range []Platform{AlibabaCloud, Smsbao, Abosend, Twilio} {
		if parsePlatform(p.String()) != p {
			t.Fatalf("%s does not round-trip", p)
		}
	}
	if parsePlatform("nope") != unsupported || unsupported.String() != "unsupported" {
		t.Fatal("unknown platforms must parse to unsupported")
	}
}
