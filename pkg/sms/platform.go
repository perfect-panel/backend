package sms

import "github.com/perfect-panel/server/internal/model/dto"

type Platform int

const (
	AlibabaCloud Platform = iota
	Smsbao
	Abosend
	Twilio
	unsupported
)

var platformNames = map[string]Platform{
	"AlibabaCloud": AlibabaCloud,
	"smsbao":       Smsbao,
	"abosend":      Abosend,
	"twilio":       Twilio,
	"unsupported":  unsupported,
}

func (p Platform) String() string {
	for k, v := range platformNames {
		if v == p {
			return k
		}
	}
	return "unsupported"
}

func parsePlatform(s string) Platform {
	if p, ok := platformNames[s]; ok {
		return p
	}
	return unsupported
}

func GetSupportedPlatforms() []dto.PlatformInfo {
	return []dto.PlatformInfo{
		{
			Platform:    AlibabaCloud.String(),
			PlatformUrl: "https://www.alibabacloud.com",
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
			PlatformUrl: "https://www.smsbao.com",
			PlatformFieldDescription: map[string]string{
				"access":        "Username",
				"secret":        "Password",
				"code_variable": "{{.code}}",
				"template":      "Your verification code is: {{.code}}",
			},
		},
		{
			Platform:    Abosend.String(),
			PlatformUrl: "https://www.abosend.com",
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
			PlatformUrl: "https://www.twilio.com",
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
