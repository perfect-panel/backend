package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// Password guessing against one account is capped: after MaxPasswordAttempts
// checks without a correct password within PasswordAttemptWindow, every flow
// that checks the account's password (sign-in, changing it, replacing a
// bound address with it) is refused until the window ends. The counter is
// keyed by the account, so spreading requests across addresses does not
// help, and the window starts at the first attempt, so a lockout never
// outlasts it. Signing in with a code and resetting the password stay open
// to the owner.
const (
	MaxPasswordAttempts   = 10
	PasswordAttemptWindow = 15 * time.Minute
)

// reserveAttemptScript counts the attempt and reports whether it is within
// the limit. Counting before the check, in one Redis script, is what makes
// the limit hold against concurrent guesses: a burst cannot read a counter
// of zero many times while its password checks queue up.
var reserveAttemptScript = redis.NewScript(`
local attempts = redis.call("INCR", KEYS[1])
if attempts == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
if attempts > tonumber(ARGV[1]) then
  return 0
end
return 1
`)

// Device sign-in guesses are capped the same way, twice over: per identifier,
// so one device cannot be guessed at, and per client address, so one client
// cannot enumerate identifiers at line speed. A device identifier is a bearer
// credential (docs/design/device-authentication.md), so its counter is keyed
// by its digest, never by the identifier itself.
const (
	MaxDeviceLoginAttempts     = MaxPasswordAttempts
	DeviceLoginAttemptWindow   = PasswordAttemptWindow
	MaxDeviceLoginIPAttempts   = 60
	DeviceLoginIPAttemptWindow = time.Hour
)

// PasswordAttemptKey is the Redis key counting the password attempts against
// the account userID in the current window.
func PasswordAttemptKey(userID int64) string {
	return "auth:password_attempts:" + strconv.FormatInt(userID, 10)
}

// DeviceLoginAttemptKey is the Redis key counting the device sign-in attempts
// with identifier in the current window.
func DeviceLoginAttemptKey(identifier string) string {
	sum := sha256.Sum256([]byte(identifier))
	return "auth:device_login_attempts:id:" + hex.EncodeToString(sum[:])
}

// DeviceLoginIPAttemptKey is the Redis key counting the device sign-in
// attempts from the client address ip in the current window; an unknown
// address shares one counter.
func DeviceLoginIPAttemptKey(ip string) string {
	if ip == "" {
		ip = "unknown"
	}
	return "auth:device_login_attempts:ip:" + ip
}

// ReservePasswordAttempt reserves one password check against the account
// userID and refuses once the window's attempts are used up. A flow calls it
// before it compares the password; only ClearPasswordAttempts, after a
// correct password, gives the attempts back before the window ends.
func ReservePasswordAttempt(ctx context.Context, client *redis.Client, userID int64) error {
	return ReserveAttempt(ctx, client, PasswordAttemptKey(userID), MaxPasswordAttempts, PasswordAttemptWindow)
}

// ReserveAttempt reserves one attempt under key and refuses with
// TooManyRequests once limit attempts were made within window of the first.
// The attempt is counted before the check it guards, so concurrent attempts
// cannot exceed the limit; ClearAttempts gives them back.
func ReserveAttempt(ctx context.Context, client *redis.Client, key string, limit int64, window time.Duration) error {
	if client == nil {
		return nil
	}
	allowed, err := reserveAttemptScript.Run(ctx, client, []string{key}, limit, window.Milliseconds()).Int64()
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "reserve attempt")
	}
	if allowed == 0 {
		return xerr.Errorf(xerr.TooManyRequests, "too many failed attempts, try again later")
	}
	return nil
}

// ClearPasswordAttempts forgets the attempts once the owner proves the
// password.
func ClearPasswordAttempts(ctx context.Context, client *redis.Client, userID int64) {
	ClearAttempts(ctx, client, PasswordAttemptKey(userID))
}

// ClearAttempts forgets the attempts counted under key once the credential
// they guarded is proven.
func ClearAttempts(ctx context.Context, client *redis.Client, key string) {
	if client == nil {
		return
	}
	if err := client.Del(ctx, key).Err(); err != nil {
		logger.WithContext(ctx).Errorw("clear attempts failed", logger.Field("error", err.Error()), logger.Field("key", key))
	}
}
