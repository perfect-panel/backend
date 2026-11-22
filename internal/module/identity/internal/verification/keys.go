package verification

import (
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
)

// EmailCodeKey is the Redis key holding the code sent to email for purpose.
// email must be canonical (identifier.CanonicalEmail), the form the sender
// and every checker use.
func EmailCodeKey(purpose auth.VerifyType, email string) string {
	return config.AuthCodeCacheKey + ":" + purpose.String() + ":" + email
}

// MobileCodeKey is the Redis key holding the code sent by SMS for purpose.
// e164 must be the number in E.164 (identifier.FormatToE164 or
// identifier.CanonicalMobile): the sender and every checker then agree
// however the client wrote the number.
func MobileCodeKey(purpose auth.VerifyType, e164 string) string {
	return config.AuthCodeTelephoneCacheKey + ":" + purpose.String() + ":" + e164
}
