package logger

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// RedactedValue replaces a redacted field value or text in the log.
const RedactedValue = "[REDACTED]"

var (
	jwtPattern              = regexp.MustCompile(`\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	telegramBotTokenPattern = regexp.MustCompile(`\b[0-9]{6,12}:AA[A-Za-z0-9_-]{20,}\b`)
	emailPattern            = regexp.MustCompile(`\b[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+\b`)
	bearerPattern           = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	sensitiveQueryPattern   = regexp.MustCompile(`(?i)([?&](?:access_?token|auth_?code|code|key|password|secret|signature|token)=)[^&#\s]+`)
)

// sensitiveFieldKey classifies fields that must never reach a log sink. Keep
// this list deliberately broad: losing a diagnostic value is preferable to
// persisting a credential, message body or personal identifier.
func sensitiveFieldKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	compact := strings.ReplaceAll(normalized, "_", "")

	for _, marker := range []string{
		"token", "secret", "password", "passwd", "credential", "authorization",
		"cookie", "signature", "privatekey", "apikey", "accesskey", "session", "uuid",
		"email", "telephone", "phone", "useragent", "clientip", "deviceid",
		"chatid", "openid", "uniqueid", "senderid",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}

	switch compact {
	case "body", "requestbody", "responsebody", "request", "response", "payload",
		"content", "query", "params", "form", "config", "headers", "header",
		"sql", "user", "users", "userinfo", "order", "orders", "orderinfo",
		"subscribe", "subscribers", "req", "template", "value", "cachekey", "coupon",
		"email", "telephone", "phone", "ip", "clientip", "useragent", "identifier",
		"openid", "uniqueid", "chatid", "senderid", "recipient", "subject", "text",
		"command", "redirect", "redirecturl", "url", "code", "refercode", "deductionlog":
		return true
	default:
		return false
	}
}

func redactText(value string) string {
	value = jwtPattern.ReplaceAllString(value, RedactedValue)
	value = telegramBotTokenPattern.ReplaceAllString(value, RedactedValue)
	value = emailPattern.ReplaceAllString(value, RedactedValue)
	value = bearerPattern.ReplaceAllString(value, "Bearer "+RedactedValue)
	return sensitiveQueryPattern.ReplaceAllString(value, `${1}`+RedactedValue)
}

func redactField(field LogField) LogField {
	if field.allowRiskMetadata && riskMetadataFieldKey(field.Key) {
		return field
	}
	if sensitiveFieldKey(field.Key) {
		field.Value = RedactedValue
		return field
	}
	field.Value = redactValue(field.Value)
	return field
}

// RiskField is the only supported escape hatch for personal metadata in the
// process log. It is intentionally limited to the two fields explicitly used
// by the HTTP risk-control audit; every other sensitive key still follows the
// global redaction policy.
func RiskField(key, value string) LogField {
	if !riskMetadataFieldKey(key) {
		return Field(key, value)
	}
	if normalizedFieldKey(key) == "clientip" {
		value = requestmeta.Bound(value, requestmeta.MaxClientIPBytes)
	} else {
		value = requestmeta.Bound(value, requestmeta.MaxUserAgentBytes)
	}
	return LogField{Key: key, Value: value, allowRiskMetadata: true}
}

func riskMetadataFieldKey(key string) bool {
	normalized := normalizedFieldKey(key)
	return normalized == "clientip" || normalized == "useragent"
}

func normalizedFieldKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	return strings.ReplaceAll(normalized, "_", "")
}

func redactFields(fields []LogField) []LogField {
	if len(fields) == 0 {
		return fields
	}
	redacted := make([]LogField, len(fields))
	for i, field := range fields {
		redacted[i] = redactField(field)
	}
	return redacted
}

func redactValue(value any) any {
	return redactValueDepth(value, 0)
}

func redactValueDepth(value any, depth int) any {
	if depth > 8 {
		return RedactedValue
	}
	switch v := value.(type) {
	case string:
		return redactText(v)
	case error:
		return redactText(v.Error())
	case fmt.Stringer:
		return redactText(encodeStringer(v))
	case []string:
		redacted := make([]string, len(v))
		for i, item := range v {
			redacted[i] = redactText(item)
		}
		return redacted
	case []byte:
		return redactText(string(v))
	case map[string]string:
		redacted := make(map[string]string, len(v))
		for key, item := range v {
			if sensitiveFieldKey(key) {
				redacted[key] = RedactedValue
			} else {
				redacted[key] = redactText(item)
			}
		}
		return redacted
	case map[string]any:
		redacted := make(map[string]any, len(v))
		for key, item := range v {
			if sensitiveFieldKey(key) {
				redacted[key] = RedactedValue
			} else {
				redacted[key] = redactValueDepth(item, depth+1)
			}
		}
		return redacted
	case []any:
		redacted := make([]any, len(v))
		for i, item := range v {
			redacted[i] = redactValueDepth(item, depth+1)
		}
		return redacted
	default:
		return value
	}
}

func limitValue(value any, maxLen uint32, depth int) (any, bool) {
	if maxLen == 0 || depth > 8 {
		return value, false
	}
	limit := int(maxLen)
	switch v := value.(type) {
	case string:
		if len(v) <= limit {
			return v, false
		}
		return v[:limit], true
	case []string:
		limited := make([]string, len(v))
		truncated := false
		for i, item := range v {
			value, cut := limitValue(item, maxLen, depth+1)
			limited[i] = value.(string)
			truncated = truncated || cut
		}
		return limited, truncated
	case map[string]string:
		limited := make(map[string]string, len(v))
		truncated := false
		for key, item := range v {
			value, cut := limitValue(item, maxLen, depth+1)
			limited[key] = value.(string)
			truncated = truncated || cut
		}
		return limited, truncated
	case map[string]any:
		limited := make(map[string]any, len(v))
		truncated := false
		for key, item := range v {
			value, cut := limitValue(item, maxLen, depth+1)
			limited[key] = value
			truncated = truncated || cut
		}
		return limited, truncated
	case []any:
		limited := make([]any, len(v))
		truncated := false
		for i, item := range v {
			value, cut := limitValue(item, maxLen, depth+1)
			limited[i] = value
			truncated = truncated || cut
		}
		return limited, truncated
	default:
		return value, false
	}
}

// splitLogArgs preserves the convenient logger.Info("message", Field(...))
// call style used by older code while keeping the fields structured and
// therefore subject to the same redaction policy as InfoW/ErrorW calls.
func splitLogArgs(args []any) (string, []LogField) {
	message := make([]any, 0, len(args))
	fields := make([]LogField, 0, len(args))
	for _, arg := range args {
		switch v := arg.(type) {
		case LogField:
			fields = append(fields, redactField(v))
		case []LogField:
			fields = append(fields, redactFields(v)...)
		default:
			message = append(message, arg)
		}
	}
	return redactText(fmt.Sprint(message...)), fields
}
