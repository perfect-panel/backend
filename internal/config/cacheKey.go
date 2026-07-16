package config

// CurrencyConfigKey Currency Config Key
const CurrencyConfigKey = "system:currency_config"

// SmsConfigKey Mobile Config Key
const SmsConfigKey = "system:sms_config"

// SiteConfigKey Site Config Key
const SiteConfigKey = "system:site_config"

// SubscribeConfigKey Subscribe Config Key
const SubscribeConfigKey = "system:subscribe_config"

// RegisterConfigKey Register Config Key
const RegisterConfigKey = "system:register_config"

// VerifyConfigKey Verify Config Key
const VerifyConfigKey = "system:verify_config"

// EmailSmtpConfigKey Email Smtp Config Key
const EmailSmtpConfigKey = "system:email_smtp_config"

// NodeConfigKey Node Config Key
const NodeConfigKey = "system:node_config"

// InviteConfigKey Invite Config Key
const InviteConfigKey = "system:invite_config"

// TelegramConfigKey Telegram Config Key
const TelegramConfigKey = "system:telegram_config"

// AdminTelegramChatIdsKey cached admin Telegram chat ID list (not system config)
const AdminTelegramChatIdsKey = "system:telegram_admin_chat_ids"

// TosConfigKey Tos配置
const TosConfigKey = "system:tos_config"

// VerifyCodeConfigKey Verify Code Config Key
const VerifyCodeConfigKey = "system:verify_code_config"

// SessionIdKey cache session key
const SessionIdKey = "auth:session_id"

// GlobalConfigKey Global Config Key
const GlobalConfigKey = "system:global_config"

// AuthCodeCacheKey Register Code Cache Key
const AuthCodeCacheKey = "auth:verify:email"

// AuthCodeTelephoneCacheKey Register Code Cache Key
const AuthCodeTelephoneCacheKey = "auth:verify:telephone"

// CommonStatCacheKey CommonStat Cache Key
const CommonStatCacheKey = "common:stat"

// ServerCountCacheKey Server Count Cache Key
const ServerCountCacheKey = "server:count"

// SendIntervalKeyPrefix Auth Code Send Interval Key Prefix
const SendIntervalKeyPrefix = "send:interval:"

// SendCountLimitKeyPrefix Send Count Limit Key Prefix eg. send:limit:register:email:xxx@ppanel.dev
const SendCountLimitKeyPrefix = "send:limit:"

// RegisterIPLimitKeyPrefix limits new accounts created by the same client IP.
const RegisterIPLimitKeyPrefix = "register:limit:ip:"

// VerifyCodeAttemptKeyPrefix limits guesses against a verification code.
const VerifyCodeAttemptKeyPrefix = "auth:verify:attempt:"
