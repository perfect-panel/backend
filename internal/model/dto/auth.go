package dto

type AppleLoginCallbackRequest struct {
	Code    string `form:"code"`
	IDToken string `form:"id_token"`
	State   string `form:"state"`
}

type AuthConfig struct {
	Mobile   MobileAuthenticateConfig `json:"mobile"`
	Email    EmailAuthticateConfig    `json:"email"`
	Device   DeviceAuthticateConfig   `json:"device"`
	Register PubilcRegisterConfig     `json:"register"`
}

type AuthMethodConfig struct {
	Id      int64       `json:"id"`
	Method  string      `json:"method"`
	Config  interface{} `json:"config"`
	Enabled bool        `json:"enabled"`
}

type BindOAuthCallbackRequest struct {
	Method   string      `json:"method"`
	Callback interface{} `json:"callback"`
}

type BindOAuthRequest struct {
	Method   string `json:"method"`
	Redirect string `json:"redirect"`
}

type BindOAuthResponse struct {
	Redirect string `json:"redirect"`
}

type BindTelegramResponse struct {
	Url       string `json:"url"`
	ExpiredAt int64  `json:"expired_at"`
}

type CheckUserRequest struct {
	Email string `form:"email" validate:"required"`
}

type CheckUserResponse struct {
	Exist bool `json:"exist"`
}

type CheckVerificationCodeRequest struct {
	Method  string `json:"method" validate:"required,oneof=email mobile"`
	Account string `json:"account" validate:"required"`
	Code    string `json:"code" validate:"required"`
	Type    uint8  `json:"type" validate:"required"`
}

type CheckVerificationCodeRespone struct {
	Status bool `json:"status"`
}

type DeviceAuthticateConfig struct {
	Enable         bool `json:"enable"`
	ShowAds        bool `json:"show_ads"`
	EnableSecurity bool `json:"enable_security"`
	OnlyRealDevice bool `json:"only_real_device"`
}

type DeviceLoginRequest struct {
	Identifier string `json:"identifier" validate:"required"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `json:"user_agent" validate:"required"`
	CfToken    string `json:"cf_token,optional"`
}

type EmailAuthticateConfig struct {
	Enable             bool   `json:"enable"`
	EnableVerify       bool   `json:"enable_verify"`
	EnableDomainSuffix bool   `json:"enable_domain_suffix"`
	DomainSuffixList   string `json:"domain_suffix_list"`
}

type GetAuthMethodConfigRequest struct {
	Method string `form:"method"`
}

type GetAuthMethodListResponse struct {
	List []AuthMethodConfig `json:"list"`
}

type GetOAuthMethodsResponse struct {
	Methods []UserAuthMethod `json:"methods"`
}

type GoogleLoginCallbackRequest struct {
	Code  string `form:"code"`
	State string `form:"state"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type MobileAuthenticateConfig struct {
	Enable          bool     `json:"enable"`
	EnableWhitelist bool     `json:"enable_whitelist"`
	Whitelist       []string `json:"whitelist"`
}

type OAthLoginRequest struct {
	Method   string `json:"method" validate:"required"` // google, facebook, apple, telegram, github etc.
	Redirect string `json:"redirect"`
}

type OAuthLoginGetTokenRequest struct {
	Method   string      `json:"method" validate:"required"` // google, facebook, apple, telegram, github etc.
	Callback interface{} `json:"callback" validate:"required"`
}

type OAuthLoginResponse struct {
	Redirect string `json:"redirect"`
}

type PubilcRegisterConfig struct {
	StopRegister            bool  `json:"stop_register"`
	EnableIpRegisterLimit   bool  `json:"enable_ip_register_limit"`
	IpRegisterLimit         int64 `json:"ip_register_limit"`
	IpRegisterLimitDuration int64 `json:"ip_register_limit_duration"`
}

type PubilcVerifyCodeConfig struct {
	VerifyCodeInterval int64 `json:"verify_code_interval"`
}

type ResetPasswordRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email" validate:"required"`
	Password   string `json:"password" validate:"required"`
	Code       string `json:"code,optional"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `header:"User-Agent"`
	LoginType  string `header:"Login-Type"`
	CfToken    string `json:"cf_token,optional"`
}

type SendCodeRequest struct {
	Email string `json:"email" validate:"required"`
	Type  uint8  `json:"type" validate:"required"`
}

type SendCodeResponse struct {
	Code   string `json:"code,omitempty"`
	Status bool   `json:"status"`
}

type SendSmsCodeRequest struct {
	Type              uint8  `json:"type" validate:"required"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
}

type TelegramConfig struct {
	TelegramBotToken      string `json:"telegram_bot_token"`
	TelegramGroupUrl      string `json:"telegram_group_url"`
	TelegramNotify        bool   `json:"telegram_notify"`
	TelegramWebHookDomain string `json:"telegram_web_hook_domain"`
}

type TelephoneCheckUserRequest struct {
	Telephone         string `form:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
}

type TelephoneCheckUserResponse struct {
	Exist bool `json:"exist"`
}

type TelephoneLoginRequest struct {
	Identifier        string `json:"identifier"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneCode     string `json:"telephone_code"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
	Password          string `json:"password"`
	IP                string `header:"X-Original-Forwarded-For"`
	UserAgent         string `header:"User-Agent"`
	LoginType         string `header:"Login-Type"`
	CfToken           string `json:"cf_token,optional"`
}

type TelephoneRegisterRequest struct {
	Identifier        string `json:"identifier"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
	Password          string `json:"password" validate:"required"`
	Invite            string `json:"invite,optional"`
	Code              string `json:"code,optional"`
	IP                string `header:"X-Original-Forwarded-For"`
	UserAgent         string `header:"User-Agent"`
	LoginType         string `header:"Login-Type,optional"`
	CfToken           string `json:"cf_token,optional"`
}

type TelephoneResetPasswordRequest struct {
	Identifier        string `json:"identifier"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
	Password          string `json:"password" validate:"required"`
	Code              string `json:"code,optional"`
	IP                string `header:"X-Original-Forwarded-For"`
	UserAgent         string `header:"User-Agent"`
	LoginType         string `header:"Login-Type,optional"`
	CfToken           string `json:"cf_token,optional"`
}

type TestEmailSendRequest struct {
	Email string `json:"email" validate:"required"`
}

type TestSmsSendRequest struct {
	AreaCode  string `json:"area_code" validate:"required"`
	Telephone string `json:"telephone" validate:"required"`
}

type UnbindOAuthRequest struct {
	Method string `json:"method"`
}

type UpdateAuthMethodConfigRequest struct {
	Id      int64       `json:"id"`
	Method  string      `json:"method"`
	Config  interface{} `json:"config"`
	Enabled *bool       `json:"enabled"`
}

type UserLoginRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email" validate:"required"`
	Password   string `json:"password" validate:"required"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `header:"User-Agent"`
	LoginType  string `header:"Login-Type"`
	CfToken    string `json:"cf_token,optional"`
}

type UserRegisterRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email" validate:"required"`
	Password   string `json:"password" validate:"required"`
	Invite     string `json:"invite,optional"`
	Code       string `json:"code,optional"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `header:"User-Agent"`
	LoginType  string `header:"Login-Type"`
	CfToken    string `json:"cf_token,optional"`
}

type VerifyEmailRequest struct {
	Email string `json:"email" validate:"required"`
	Code  string `json:"code" validate:"required"`
}
