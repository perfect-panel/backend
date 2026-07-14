package auth

import (
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/pkg/email"
)

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

// Filter auth 列表查询过滤条件
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
	return json.Unmarshal([]byte(data), &l)
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
	return json.Unmarshal([]byte(data), &l)
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
	return json.Unmarshal([]byte(data), &l)
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
	return json.Unmarshal([]byte(data), &l)
}

type TelegramAuthConfig struct {
	BotToken      string `json:"bot_token"`
	EnableNotify  bool   `json:"enable_notify"`
	WebHookDomain string `json:"webhook_domain"`
}

func (l *TelegramAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		bytes, _ = json.Marshal(new(TelegramAuthConfig))
	}
	return string(bytes)
}

func (l *TelegramAuthConfig) Unmarshal(data string) error {
	return json.Unmarshal([]byte(data), &l)
}

type EmailAuthConfig struct {
	Platform                   string      `json:"platform"`
	PlatformConfig             interface{} `json:"platform_config"`
	EnableVerify               bool        `json:"enable_verify"`
	EnableNotify               bool        `json:"enable_notify"`
	EnableDomainSuffix         bool        `json:"enable_domain_suffix"`
	DomainSuffixList           string      `json:"domain_suffix_list"`
	VerifyEmailTemplate        string      `json:"verify_email_template"`
	ExpirationEmailTemplate    string      `json:"expiration_email_template"`
	MaintenanceEmailTemplate   string      `json:"maintenance_email_template"`
	TrafficExceedEmailTemplate string      `json:"traffic_exceed_email_template"`
}

func (l *EmailAuthConfig) Marshal() string {
	if l.ExpirationEmailTemplate == "" {
		l.ExpirationEmailTemplate = email.DefaultExpirationEmailTemplate
	}
	if l.ExpirationEmailTemplate == "" {
		l.MaintenanceEmailTemplate = email.DefaultMaintenanceEmailTemplate
	}
	if l.TrafficExceedEmailTemplate == "" {
		l.TrafficExceedEmailTemplate = email.DefaultTrafficExceedEmailTemplate
	}
	if l.VerifyEmailTemplate == "" {
		l.VerifyEmailTemplate = email.DefaultEmailVerifyTemplate
	}
	bytes, err := json.Marshal(l)
	if err != nil {
		config := &EmailAuthConfig{
			Platform:                   "smtp",
			PlatformConfig:             new(SMTPConfig),
			EnableVerify:               true,
			EnableNotify:               true,
			EnableDomainSuffix:         false,
			DomainSuffixList:           "",
			VerifyEmailTemplate:        email.DefaultEmailVerifyTemplate,
			ExpirationEmailTemplate:    email.DefaultExpirationEmailTemplate,
			MaintenanceEmailTemplate:   email.DefaultMaintenanceEmailTemplate,
			TrafficExceedEmailTemplate: email.DefaultTrafficExceedEmailTemplate,
		}

		bytes, _ = json.Marshal(config)
	}
	return string(bytes)
}

func (l *EmailAuthConfig) Unmarshal(data string) {
	err := json.Unmarshal([]byte(data), &l)
	if err != nil {
		config := &EmailAuthConfig{
			Platform:                   "smtp",
			PlatformConfig:             new(SMTPConfig),
			EnableVerify:               true,
			EnableNotify:               true,
			EnableDomainSuffix:         false,
			DomainSuffixList:           "",
			VerifyEmailTemplate:        email.DefaultEmailVerifyTemplate,
			ExpirationEmailTemplate:    email.DefaultExpirationEmailTemplate,
			MaintenanceEmailTemplate:   email.DefaultMaintenanceEmailTemplate,
			TrafficExceedEmailTemplate: email.DefaultTrafficExceedEmailTemplate,
		}
		_ = json.Unmarshal([]byte(config.Marshal()), &l)
	}
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
	return json.Unmarshal([]byte(data), &l)
}

type MobileAuthConfig struct {
	Platform        string      `json:"platform"`
	PlatformConfig  interface{} `json:"platform_config"`
	EnableWhitelist bool        `json:"enable_whitelist"`
	Whitelist       []string    `json:"whitelist"`
}

func (l *MobileAuthConfig) Marshal() string {
	bytes, err := json.Marshal(l)
	if err != nil {
		config := &MobileAuthConfig{
			Platform:        "alibaba_cloud",
			PlatformConfig:  new(AlibabaCloudConfig),
			EnableWhitelist: false,
			Whitelist:       []string{},
		}
		bytes, _ = json.Marshal(config)
	}
	return string(bytes)
}

func (l *MobileAuthConfig) Unmarshal(data string) {
	err := json.Unmarshal([]byte(data), &l)
	if err != nil {
		config := &MobileAuthConfig{
			Platform:        "alibaba_cloud",
			PlatformConfig:  new(AlibabaCloudConfig),
			EnableWhitelist: false,
			Whitelist:       []string{},
		}
		_ = json.Unmarshal([]byte(config.Marshal()), &l)
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
	ShowAds        bool   `json:"show_ads"`
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
