// Package sms sends text messages, verification codes above all, through
// the provider the administrators configure. The providers live in the
// subpackages; Sender hides which one is in force and renders the message
// template for the providers that take a finished text.
package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/perfect-panel/backend/internal/infra/integration"
	"github.com/perfect-panel/backend/internal/infra/sms/abosend"
	"github.com/perfect-panel/backend/internal/infra/sms/alibabacloud"
	"github.com/perfect-panel/backend/internal/infra/sms/smsbao"
	"github.com/perfect-panel/backend/internal/infra/sms/twilio"
	"github.com/perfect-panel/backend/pkg/templatex"
)

// Message is one text message to a phone number.
type Message struct {
	// Area is the country calling code, without the plus sign ("86").
	Area string
	// Mobile is the number within the area.
	Mobile string
	// Params fill the message template: the text template kept in the
	// provider configuration, or for Alibaba Cloud the template approved on
	// the provider's side. A verification code travels as "code".
	Params map[string]string
}

// CodeMessage is the message that delivers a verification code.
func CodeMessage(area, mobile, code string) Message {
	return Message{Area: area, Mobile: mobile, Params: map[string]string{"code": code}}
}

// Sender delivers text messages through one provider account. It is safe
// for concurrent use.
type Sender interface {
	// Send delivers msg; ctx bounds the provider call.
	Send(ctx context.Context, msg Message) error
}

// httpClient is the one HTTP client the providers send through, so their
// connections are pooled across messages. Its timeout bounds every provider
// call in addition to the caller's context.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// textProvider delivers a finished text; the sender renders the template.
type textProvider interface {
	SendText(ctx context.Context, area, mobile, text string) error
}

// templateProvider fills a template kept on the provider's side.
type templateProvider interface {
	SendTemplate(ctx context.Context, area, mobile string, params map[string]string) error
}

type sender struct {
	// template is the text template of a text provider.
	template string
	text     textProvider
	provided templateProvider
}

func (s *sender) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.provided != nil {
		return s.provided.SendTemplate(ctx, msg.Area, msg.Mobile, msg.Params)
	}
	text, err := render(s.template, msg.Params)
	if err != nil {
		return err
	}
	return s.text.SendText(ctx, msg.Area, msg.Mobile, text)
}

// render fills a text template with params. A template that fails to parse
// or execute is an error: the half-rendered or empty text would reach the
// recipient without the code it was meant to carry.
func render(template string, params map[string]string) (string, error) {
	data := make(map[string]any, len(params))
	for key, value := range params {
		data[key] = value
	}
	text, err := templatex.RenderToString(template, data)
	if err != nil {
		return "", fmt.Errorf("render sms template: %w", err)
	}
	return text, nil
}

// NewSender builds the provider client for one provider configuration.
// config carries the provider credentials (access keys, auth tokens,
// passwords) and must never be logged. Callers that send repeatedly keep
// their sender in a Senders cache.
func NewSender(platform, config string) (Sender, error) {
	switch parsePlatform(platform) {
	case AlibabaCloud:
		cfg := alibabacloud.Config{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return nil, fmt.Errorf("alibabacloud config unmarshal failed: %w", err)
		}
		client, err := alibabacloud.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		return &sender{provided: client}, nil
	case Abosend:
		cfg := abosend.Config{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return nil, fmt.Errorf("abosend config unmarshal failed: %w", err)
		}
		return &sender{template: cfg.Template, text: abosend.NewClient(cfg, httpClient)}, nil
	case Smsbao:
		cfg := smsbao.Config{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return nil, fmt.Errorf("smsbao config unmarshal failed: %w", err)
		}
		return &sender{template: cfg.Template, text: smsbao.NewClient(cfg, httpClient)}, nil
	case Twilio:
		cfg := twilio.Config{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return nil, fmt.Errorf("twilio config unmarshal failed: %w", err)
		}
		return &sender{template: cfg.Template, text: twilio.NewClient(cfg, httpClient)}, nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}

// Senders keeps the sender built for the provider configuration in force,
// rebuilding it when the configuration changes. The zero value is ready.
type Senders struct {
	cache integration.ClientCache[Sender]
}

// Get returns the sender for platform and config.
func (s *Senders) Get(platform, config string) (Sender, error) {
	return s.cache.Get(func() (Sender, error) { return NewSender(platform, config) }, platform, config)
}
