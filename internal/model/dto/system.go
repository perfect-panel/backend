package dto

type Currency struct {
	CurrencyUnit   string `json:"currency_unit"`
	CurrencySymbol string `json:"currency_symbol"`
}

type CurrencyConfig struct {
	AccessKey      string `json:"access_key"`
	CurrencyUnit   string `json:"currency_unit"`
	CurrencySymbol string `json:"currency_symbol"`
}

type GetGlobalConfigResponse struct {
	Site         SiteConfig             `json:"site"`
	Verify       VeifyConfig            `json:"verify"`
	Auth         AuthConfig             `json:"auth"`
	Invite       InviteConfig           `json:"invite"`
	Currency     Currency               `json:"currency"`
	Subscribe    SubscribeConfig        `json:"subscribe"`
	VerifyCode   PubilcVerifyCodeConfig `json:"verify_code"`
	OAuthMethods []string               `json:"oauth_methods"`
	WebAd        bool                   `json:"web_ad"`
}

type GetTosResponse struct {
	TosContent string `json:"tos_content"`
}

type InviteConfig struct {
	ForcedInvite       bool  `json:"forced_invite"`
	ReferralPercentage int64 `json:"referral_percentage"`
	OnlyFirstPurchase  bool  `json:"only_first_purchase"`
}

type ModuleConfig struct {
	Secret         string `json:"secret"`          // 通讯密钥
	ServiceName    string `json:"service_name"`    // 服务名称
	ServiceVersion string `json:"service_version"` // 服务版本
}

type PrivacyPolicyConfig struct {
	PrivacyPolicy string `json:"privacy_policy"`
}

type RegisterConfig struct {
	StopRegister            bool   `json:"stop_register"`
	EnableTrial             bool   `json:"enable_trial"`
	TrialSubscribe          int64  `json:"trial_subscribe"`
	TrialTime               int64  `json:"trial_time"`
	TrialTimeUnit           string `json:"trial_time_unit"`
	EnableIpRegisterLimit   bool   `json:"enable_ip_register_limit"`
	IpRegisterLimit         int64  `json:"ip_register_limit"`
	IpRegisterLimitDuration int64  `json:"ip_register_limit_duration"`
}

type SiteConfig struct {
	Host       string `json:"host"`
	SiteName   string `json:"site_name"`
	SiteDesc   string `json:"site_desc"`
	SiteLogo   string `json:"site_logo"`
	Keywords   string `json:"keywords"`
	CustomHTML string `json:"custom_html"`
	CustomData string `json:"custom_data"`
}

type SiteCustomDataContacts struct {
	Email     string `json:"email"`
	Telephone string `json:"telephone"`
	Address   string `json:"address"`
}

type TosConfig struct {
	TosContent string `json:"tos_content"`
}

type VeifyConfig struct {
	TurnstileSiteKey          string `json:"turnstile_site_key"`
	EnableLoginVerify         bool   `json:"enable_login_verify"`
	EnableRegisterVerify      bool   `json:"enable_register_verify"`
	EnableResetPasswordVerify bool   `json:"enable_reset_password_verify"`
}

type VerifyCodeConfig struct {
	VerifyCodeExpireTime int64 `json:"verify_code_expire_time"`
	VerifyCodeLimit      int64 `json:"verify_code_limit"`
	VerifyCodeInterval   int64 `json:"verify_code_interval"`
}

type VerifyConfig struct {
	TurnstileSiteKey          string `json:"turnstile_site_key"`
	TurnstileSecret           string `json:"turnstile_secret"`
	EnableLoginVerify         bool   `json:"enable_login_verify"`
	EnableRegisterVerify      bool   `json:"enable_register_verify"`
	EnableResetPasswordVerify bool   `json:"enable_reset_password_verify"`
}
