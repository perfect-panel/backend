package mail

import (
	"github.com/perfect-panel/backend/internal/infra/integration"
)

// Platform is an email provider, stored in the configuration by its name.
type Platform int

const (
	// SMTP delivers through an SMTP relay.
	SMTP Platform = iota
	unsupported
)

var platforms = integration.NewPlatforms(unsupported, map[string]Platform{
	"smtp": SMTP,
})

// String returns the name the configuration stores p under.
func (p Platform) String() string {
	return platforms.Name(p)
}

func parsePlatform(s string) Platform {
	return platforms.Parse(s)
}

// GetSupportedPlatforms describes the providers and their configuration
// fields for the administrators' settings page.
func GetSupportedPlatforms() []integration.Info {
	return []integration.Info{
		{
			Platform:    SMTP.String(),
			PlatformURL: "",
			PlatformFieldDescription: map[string]string{
				"host":     "host",
				"port":     "port",
				"user":     "user",
				"pass":     "pass",
				"from":     "from",
				"reply_to": "reply_to",
				// ssl requires encryption (implicit TLS on 465, STARTTLS
				// otherwise); implicit_tls starts with TLS on another port;
				// insecure_skip_verify accepts a self-signed relay
				// certificate. See SMTPConfig.
				"ssl":                  "ssl",
				"implicit_tls":         "implicit_tls",
				"insecure_skip_verify": "insecure_skip_verify",
			},
		},
	}
}
