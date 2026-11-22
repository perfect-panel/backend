// Package verification stores and checks the email and SMS verification
// codes of the identity module in Redis: the code of one identifier and
// purpose, and the guesses made against it.
package verification

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

// maxVerificationCodeAttempts wrong guesses delete a code.
const maxVerificationCodeAttempts = 10

var (
	// ErrVerificationCodeInvalid reports a code that does not match, was
	// never sent or expired.
	ErrVerificationCodeInvalid = errors.New("verification code is invalid or expired")
	// ErrVerificationAttemptsExceeded reports a code that was guessed at too
	// often; it is deleted.
	ErrVerificationAttemptsExceeded = errors.New("verification code attempt limit exceeded")
	verifyCodeScript                = redis.NewScript(`
local keyType = redis.call("TYPE", KEYS[1])
if type(keyType) == "table" then
  keyType = keyType["ok"]
end
local expected = nil
if keyType == "hash" then
  expected = redis.call("HGET", KEYS[1], "code")
elseif keyType == "string" then
  local ok, payload = pcall(cjson.decode, redis.call("GET", KEYS[1]))
  if ok and payload then
    expected = tostring(payload["code"])
  end
end
if not expected then
  return 0
end
local attempts = tonumber(redis.call("GET", KEYS[2]) or "0")
if attempts >= tonumber(ARGV[2]) then
  redis.call("DEL", KEYS[1])
  return -1
end
if expected ~= ARGV[1] then
  attempts = redis.call("INCR", KEYS[2])
  if attempts == 1 then
    local ttl = redis.call("TTL", KEYS[1])
    if ttl < 1 then ttl = 300 end
    redis.call("EXPIRE", KEYS[2], ttl)
  end
  if attempts >= tonumber(ARGV[2]) then
    redis.call("DEL", KEYS[1])
    return -1
  end
  return 0
end
if ARGV[3] == "1" then
  redis.call("DEL", KEYS[1], KEYS[2])
end
return 1
`)
)

func verificationAttemptKey(cacheKey string) string {
	return config.VerifyCodeAttemptKeyPrefix + cacheKey
}

// SaveVerificationCode stores code under cacheKey for expiration, five
// minutes when it is not positive, and forgets the guesses made against the
// previous code.
func SaveVerificationCode(ctx context.Context, client *redis.Client, cacheKey, code string, expiration time.Duration) error {
	if expiration <= 0 {
		expiration = 5 * time.Minute
	}
	_, err := client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// Remove the legacy string payload before replacing it with the hash
		// representation used for atomic validation.
		pipe.Del(ctx, cacheKey)
		pipe.HSet(ctx, cacheKey, "code", code, "last_at", time.Now().Unix())
		pipe.Expire(ctx, cacheKey, expiration)
		pipe.Del(ctx, verificationAttemptKey(cacheKey))
		return nil
	})
	return err
}

// DeleteVerificationCode removes the code under cacheKey and its guesses.
func DeleteVerificationCode(ctx context.Context, client *redis.Client, cacheKey string) error {
	return client.Del(ctx, cacheKey, verificationAttemptKey(cacheKey)).Err()
}

// ValidateVerificationCode rate-limits guesses and optionally consumes a
// correct code atomically. A registration or account mutation must consume it;
// a UI pre-check may validate without consuming it.
func ValidateVerificationCode(ctx context.Context, client *redis.Client, cacheKey, supplied string, consume bool) error {
	result, err := verifyCodeScript.Run(ctx, client,
		[]string{cacheKey, verificationAttemptKey(cacheKey)},
		supplied, strconv.Itoa(maxVerificationCodeAttempts), boolString(consume),
	).Int()
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case -1:
		return ErrVerificationAttemptsExceeded
	default:
		return ErrVerificationCodeInvalid
	}
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
