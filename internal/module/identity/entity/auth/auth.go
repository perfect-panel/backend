// Package auth holds the identity module's authentication-method entities:
// the stored configuration of each sign-in method and the purposes of
// verification codes.
package auth

import (
	"encoding/json"
	"time"

	"github.com/perfect-panel/backend/internal/infra/mail"
)

// Auth is the stored configuration of one authentication method: email,
// mobile, device or an OAuth provider.
type Auth struct {
	Id        int64     `gorm:"primaryKey"`
	Method    string    `gorm:"unique;type:varchar(255);not null;default:'';comment:platform"`
	Config    string    `gorm:"type:text;not null;comment:Auth Configuration"`
	Enabled   *bool     `gorm:"type:tinyint(1);not null;default:false;comment:Is Enabled"`
	CreatedAt time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (Auth) TableName() string {
	return "auth_method"
}

// Filter filters the auth method list.
type Filter struct {
	Show   *bool
	Pinned *bool
	Popup  *bool
	Search string
}

type AppleAuthConfig struct {
	TeamID       string `json:"team_id"`
	KeyID        string `json:"key_id"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

func (l *AppleAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(AppleAuthConfig))
	}
	return string(bytes)
}

func (l *AppleAuthConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type GoogleAuthConfig struct {
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

func (l *GoogleAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(GoogleAuthConfig))
	}
	return string(bytes)
}

func (l *GoogleAuthConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type GithubAuthConfig struct {
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

func (l *GithubAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(GithubAuthConfig))
	}
	return string(bytes)
}

func (l *GithubAuthConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type FacebookAuthConfig struct {
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

func (l *FacebookAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(FacebookAuthConfig))
	}
	return string(bytes)
}

func (l *FacebookAuthConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type TelegramAuthConfig struct {
	BotToken      string `json:"bot_token"`
	EnableNotify  bool   `json:"enable_notify"`
	WebHookDomain string `json:"webhook_domain"`
	// GroupChatID is the administrators' forum-enabled supergroup (e.g.
	// "-1001234567890"). It is a string because the admin panel round-trips
	// this config as JSON text. Empty disables every group feature.
	GroupChatID string `json:"group_chat_id"`
}

func (l *TelegramAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(TelegramAuthConfig))
	}
	return string(bytes)
}

func (l *TelegramAuthConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type EmailAuthConfig struct {
	Platform                   string `json:"platform"`
	PlatformConfig             any    `json:"platform_config"`
	EnableVerify               bool   `json:"enable_verify"`
	EnableNotify               bool   `json:"enable_notify"`
	EnableDomainSuffix         bool   `json:"enable_domain_suffix"`
	DomainSuffixList           string `json:"domain_suffix_list"`
	VerifyEmailTemplate        string `json:"verify_email_template"`
	ExpirationEmailTemplate    string `json:"expiration_email_template"`
	MaintenanceEmailTemplate   string `json:"maintenance_email_template"`
	TrafficExceedEmailTemplate string `json:"traffic_exceed_email_template"`
	// Subjects pair with the templates above; an empty subject falls back to
	// the delivery-time default, so pre-existing configs keep sending the
	// original English subjects.
	VerifyEmailSubject        string `json:"verify_email_subject"`
	ExpirationEmailSubject    string `json:"expiration_email_subject"`
	MaintenanceEmailSubject   string `json:"maintenance_email_subject"`
	TrafficExceedEmailSubject string `json:"traffic_exceed_email_subject"`
}

func (l *EmailAuthConfig) Marshal() string {
	if l.ExpirationEmailTemplate == "" {
		l.ExpirationEmailTemplate = mail.DefaultExpirationEmailTemplate
	}
	if l.MaintenanceEmailTemplate == "" {
		l.MaintenanceEmailTemplate = mail.DefaultMaintenanceEmailTemplate
	}
	if l.TrafficExceedEmailTemplate == "" {
		l.TrafficExceedEmailTemplate = mail.DefaultTrafficExceedEmailTemplate
	}
	if l.VerifyEmailTemplate == "" {
		l.VerifyEmailTemplate = mail.DefaultEmailVerifyTemplate
	}
	if l.VerifyEmailSubject == "" {
		l.VerifyEmailSubject = mail.DefaultEmailVerifySubject
	}
	if l.ExpirationEmailSubject == "" {
		l.ExpirationEmailSubject = mail.DefaultExpirationEmailSubject
	}
	if l.MaintenanceEmailSubject == "" {
		l.MaintenanceEmailSubject = mail.DefaultMaintenanceEmailSubject
	}
	if l.TrafficExceedEmailSubject == "" {
		l.TrafficExceedEmailSubject = mail.DefaultTrafficExceedEmailSubject
	}
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(defaultEmailAuthConfig())
	}
	return string(bytes)
}

// defaultEmailAuthConfig is the configuration of a fresh installation: SMTP
// with verification and notification on and the built-in templates.
func defaultEmailAuthConfig() *EmailAuthConfig {
	return &EmailAuthConfig{
		Platform:                   "smtp",
		PlatformConfig:             new(SMTPConfig),
		EnableVerify:               true,
		EnableNotify:               true,
		EnableDomainSuffix:         false,
		DomainSuffixList:           "",
		VerifyEmailTemplate:        mail.DefaultEmailVerifyTemplate,
		ExpirationEmailTemplate:    mail.DefaultExpirationEmailTemplate,
		MaintenanceEmailTemplate:   mail.DefaultMaintenanceEmailTemplate,
		TrafficExceedEmailTemplate: mail.DefaultTrafficExceedEmailTemplate,
		VerifyEmailSubject:         mail.DefaultEmailVerifySubject,
		ExpirationEmailSubject:     mail.DefaultExpirationEmailSubject,
		MaintenanceEmailSubject:    mail.DefaultMaintenanceEmailSubject,
		TrafficExceedEmailSubject:  mail.DefaultTrafficExceedEmailSubject,
	}
}

// Unmarshal decodes data into l. When data does not parse it returns the
// error and leaves l at the defaults, so a caller that has to keep running
// still holds a usable configuration.
func (l *EmailAuthConfig) Unmarshal(data string) error {
	if err := json.Unmarshal([]byte(data), l); err != nil {
		*l = EmailAuthConfig{}
		_ = json.Unmarshal([]byte(defaultEmailAuthConfig().Marshal()), l)
		return err
	}
	return nil
}

// SMTPConfig Email SMTP configuration
type SMTPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	Pass string `json:"pass"`
	From string `json:"from"`
	SSL  bool   `json:"ssl"`
}

func (l *SMTPConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(SMTPConfig))
	}
	return string(bytes)
}

func (l *SMTPConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type MobileAuthConfig struct {
	Platform        string   `json:"platform"`
	PlatformConfig  any      `json:"platform_config"`
	EnableWhitelist bool     `json:"enable_whitelist"`
	Whitelist       []string `json:"whitelist"`
}

func (l *MobileAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(defaultMobileAuthConfig())
	}
	return string(bytes)
}

// Unmarshal decodes data into l. When data does not parse it returns the
// error and leaves l at the defaults, so a caller that has to keep running
// still holds a usable configuration.
func (l *MobileAuthConfig) Unmarshal(data string) error {
	if err := json.Unmarshal([]byte(data), l); err != nil {
		*l = MobileAuthConfig{}
		_ = json.Unmarshal([]byte(defaultMobileAuthConfig().Marshal()), l)
		return err
	}
	return nil
}

// defaultMobileAuthConfig is the configuration of a fresh installation.
func defaultMobileAuthConfig() *MobileAuthConfig {
	return &MobileAuthConfig{
		Platform:        "alibaba_cloud",
		PlatformConfig:  new(AlibabaCloudConfig),
		EnableWhitelist: false,
		Whitelist:       []string{},
	}
}

type AlibabaCloudConfig struct {
	Access       string `json:"access"`
	Secret       string `json:"secret"`
	SignName     string `json:"sign_name"`
	Endpoint     string `json:"endpoint"`
	TemplateCode string `json:"template_code"`
}

func (l *AlibabaCloudConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(AlibabaCloudConfig))
	}
	return string(bytes)
}

func (l *AlibabaCloudConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type SmsbaoConfig struct {
	Access   string `json:"access"`
	Secret   string `json:"secret"`
	Template string `json:"template"`
}

func (l *SmsbaoConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(SmsbaoConfig))
	}
	return string(bytes)
}

func (l *SmsbaoConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type AbosendConfig struct {
	ApiDomain string `json:"api_domain"`
	Access    string `json:"access"`
	Secret    string `json:"secret"`
	Template  string `json:"template"`
}

func (l *AbosendConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(AbosendConfig))
	}
	return string(bytes)
}

func (l *AbosendConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type TwilioConfig struct {
	Access      string `json:"access"`
	Secret      string `json:"secret"`
	PhoneNumber string `json:"phone_number"`
	Template    string `json:"template"`
}

func (l *TwilioConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(TwilioConfig))
	}
	return string(bytes)
}

func (l *TwilioConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}

type DeviceConfig struct {
	ShowAds bool `json:"show_ads"`
	// OnlyRealDevice is the legacy flag requiring authenticated device transport.
	// It is not hardware or application attestation.
	OnlyRealDevice bool   `json:"only_real_device"`
	EnableSecurity bool   `json:"enable_security"`
	SecuritySecret string `json:"security_secret"`
}

func (l *DeviceConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(DeviceConfig))
	}
	return string(bytes)
}

func (l *DeviceConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), l)
}
