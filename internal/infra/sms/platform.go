package sms

import (
	"github.com/perfect-panel/backend/internal/infra/integration"
)

// Platform is an SMS provider, stored in the configuration by its name.
type Platform int

// The supported providers.
const (
	AlibabaCloud Platform = iota
	Smsbao
	Abosend
	Twilio
	unsupported
)

var platforms = integration.NewPlatforms(unsupported, map[string]Platform{
	"AlibabaCloud": AlibabaCloud,
	"smsbao":       Smsbao,
	"abosend":      Abosend,
	"twilio":       Twilio,
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
			Platform:    AlibabaCloud.String(),
			PlatformURL: "https://www.alibabacloud.com",
			PlatformFieldDescription: map[string]string{
				"access":        "AccessKeyId",
				"secret":        "AccessKeySecret",
				"template_code": "TemplateCode",
				"sign_name":     "SignName",
				"endpoint":      "Endpoint",
			},
		},
		{
			Platform:    Smsbao.String(),
			PlatformURL: "https://www.smsbao.com",
			PlatformFieldDescription: map[string]string{
				"access":        "Username",
				"secret":        "Password",
				"code_variable": "{{.code}}",
				"template":      "Your verification code is: {{.code}}",
			},
		},
		{
			Platform:    Abosend.String(),
			PlatformURL: "https://www.abosend.com",
			PlatformFieldDescription: map[string]string{
				"access":        "OrgCode",
				"secret":        "MD5Key",
				"code_variable": "{{.code}}",
				"template":      "Your verification code is: {{.code}}",
				"api_domain":    "https://smsapi.abosend.com",
			},
		},
		{
			Platform:    Twilio.String(),
			PlatformURL: "https://www.twilio.com",
			PlatformFieldDescription: map[string]string{
				"access":        "AccessSID",
				"secret":        "AuthToken",
				"phone_number":  "Sending phone number",
				"code_variable": "{{.code}}",
				"template":      "Your verification code is: {{.code}}",
			},
		},
	}
}
