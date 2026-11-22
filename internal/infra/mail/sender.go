// Package mail sends the application's email through the provider the
// administrators configure; SMTP is the one provider. It also holds the
// default subjects and HTML templates of the notification emails, which
// the stored configuration falls back to.
package mail

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/backend/internal/infra/integration"
)

// Sender delivers email through one provider account. It is safe for
// concurrent use.
type Sender interface {
	// SendContext delivers an HTML body to the recipients. ctx bounds the
	// whole delivery; the provider applies its own deadline besides.
	SendContext(ctx context.Context, to []string, subject, body string) error
}

// NewSender builds the provider client for one provider configuration.
// config carries the provider credentials and must never be logged. Callers
// that send repeatedly keep their sender in a Senders cache.
func NewSender(platform, config, siteName string) (Sender, error) {
	switch parsePlatform(platform) {
	case SMTP:
		cfg := SMTPConfig{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return nil, fmt.Errorf("smtp config unmarshal failed: %w", err)
		}
		cfg.SiteName = siteName
		return NewSMTPClient(&cfg), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}

// Senders keeps the sender built for the provider configuration in force,
// rebuilding it when the configuration or the site name changes. The zero
// value is ready.
type Senders struct {
	cache integration.ClientCache[Sender]
}

// Get returns the sender for platform, config and siteName.
func (s *Senders) Get(platform, config, siteName string) (Sender, error) {
	return s.cache.Get(func() (Sender, error) { return NewSender(platform, config, siteName) }, platform, config, siteName)
}
