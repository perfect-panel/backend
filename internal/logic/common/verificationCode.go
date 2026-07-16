package common

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/redis/go-redis/v9"
)

const maxVerificationCodeAttempts = 10

var (
	ErrVerificationCodeInvalid      = errors.New("verification code is invalid or expired")
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
