package auth

import (
	"testing"

	"github.com/perfect-panel/backend/internal/infra/mail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fresh config marshals with every template and subject defaulted; the
// maintenance template used to stay empty because its default was guarded by
// a copy-pasted check of the expiration template.
func TestEmailAuthConfigMarshalFillsAllDefaults(t *testing.T) {
	cfg := new(EmailAuthConfig)
	roundTripped := new(EmailAuthConfig)
	require.NoError(t, roundTripped.Unmarshal(cfg.Marshal()))

	assert.Equal(t, mail.DefaultEmailVerifyTemplate, roundTripped.VerifyEmailTemplate)
	assert.Equal(t, mail.DefaultExpirationEmailTemplate, roundTripped.ExpirationEmailTemplate)
	assert.Equal(t, mail.DefaultMaintenanceEmailTemplate, roundTripped.MaintenanceEmailTemplate)
	assert.Equal(t, mail.DefaultTrafficExceedEmailTemplate, roundTripped.TrafficExceedEmailTemplate)
	assert.Equal(t, mail.DefaultEmailVerifySubject, roundTripped.VerifyEmailSubject)
	assert.Equal(t, mail.DefaultExpirationEmailSubject, roundTripped.ExpirationEmailSubject)
	assert.Equal(t, mail.DefaultMaintenanceEmailSubject, roundTripped.MaintenanceEmailSubject)
	assert.Equal(t, mail.DefaultTrafficExceedEmailSubject, roundTripped.TrafficExceedEmailSubject)
}

func TestEmailAuthConfigMarshalKeepsCustomizedValues(t *testing.T) {
	cfg := &EmailAuthConfig{
		MaintenanceEmailTemplate: "<p>自定义维护正文</p>",
		ExpirationEmailSubject:   "【{{.SiteName}}】订阅已到期",
	}
	roundTripped := new(EmailAuthConfig)
	require.NoError(t, roundTripped.Unmarshal(cfg.Marshal()))

	assert.Equal(t, "<p>自定义维护正文</p>", roundTripped.MaintenanceEmailTemplate)
	assert.Equal(t, "【{{.SiteName}}】订阅已到期", roundTripped.ExpirationEmailSubject)
}

func TestAlibabaCloudConfig_Marshal(t *testing.T) {
	v := new(AlibabaCloudConfig)
	t.Log(v.Marshal())
}

func TestAlibabaCloudConfig_Unmarshal(t *testing.T) {

	cfg := AlibabaCloudConfig{
		Access:       "AccessKeyId",
		Secret:       "AccessKeySecret",
		SignName:     "SignName",
		Endpoint:     "Endpoint",
		TemplateCode: "VerifyTemplateCode",
	}
	data := cfg.Marshal()
	v := new(AlibabaCloudConfig)
	err := v.Unmarshal(data)
	if err != nil {
		t.Fatal(err.Error())
	}
	assert.Equal(t, "AccessKeyId", v.Access)
}

// A configuration that does not parse is reported, not silently replaced;
// the receiver still ends up at the defaults for callers that must go on.
func TestEmailAndMobileConfigUnmarshalReportParseErrors(t *testing.T) {
	email := new(EmailAuthConfig)
	if err := email.Unmarshal(`{"enable_verify":"yes"}`); err == nil {
		t.Fatal("EmailAuthConfig.Unmarshal accepted a config that does not parse")
	}
	assert.Equal(t, "smtp", email.Platform)
	assert.True(t, email.EnableVerify)
	assert.Equal(t, mail.DefaultEmailVerifyTemplate, email.VerifyEmailTemplate)

	mobile := new(MobileAuthConfig)
	if err := mobile.Unmarshal(`{"whitelist":"86"}`); err == nil {
		t.Fatal("MobileAuthConfig.Unmarshal accepted a config that does not parse")
	}
	assert.Equal(t, "alibaba_cloud", mobile.Platform)
	assert.Empty(t, mobile.Whitelist)

	if err := mobile.Unmarshal(`{"platform":"twilio","enable_whitelist":true,"whitelist":["86"]}`); err != nil {
		t.Fatalf("valid mobile config: %v", err)
	}
	assert.Equal(t, "twilio", mobile.Platform)
	assert.Equal(t, []string{"86"}, mobile.Whitelist)
}
