// Package oauthstate owns the OAuth state round trip: the state issued with
// an authorization URL, stored with the redirect and the purpose it belongs
// to, and redeemed once by the callback of that purpose. It also pins
// redirects to the site host and makes signed callbacks without a state
// single use.
package oauthstate

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/perfect-panel/backend/pkg/random"
	"github.com/redis/go-redis/v9"
)

// TTL bounds how long a sign-in may take between the authorization URL and
// its callback.
const TTL = 5 * time.Minute

var (
	// ErrUnknown reports a state that was never issued, was already redeemed
	// or expired.
	ErrUnknown = errors.New("oauth state is unknown, used or expired")
	// ErrScope reports a state redeemed by another flow than the one it was
	// issued for: a sign-in state on a binding callback, or a binding state
	// on a sign-in or on another account's binding.
	ErrScope = errors.New("oauth state was issued for another purpose or account")
	// ErrNonce reports a state redeemed by another client than the one that
	// started the round trip: the nonce the client presents does not match
	// the one the state was issued with, or one side has a nonce and the
	// other has none.
	ErrNonce = errors.New("oauth state was started by another client")
)

// Purpose is what a state was issued for.
type Purpose string

const (
	// Login states sign an account in or register one.
	Login Purpose = "login"
	// Bind states bind the identity to the signed-in account.
	Bind Purpose = "bind"
)

// Scope ties a state to the flow that issued it: its purpose and, for a
// binding, the account. A callback redeems a state only within the scope it
// was issued in, so an attacker's code and state cannot be completed on a
// victim's binding, nor a binding state on a sign-in.
type Scope struct {
	Purpose Purpose
	// UserID is the account a binding is for; 0 for a sign-in.
	UserID int64
}

// LoginScope is the scope of a sign-in.
func LoginScope() Scope { return Scope{Purpose: Login} }

// BindScope is the scope of binding an identity to the account userID.
func BindScope(userID int64) Scope { return Scope{Purpose: Bind, UserID: userID} }

// record is what a state stores.
type record struct {
	Redirect string  `json:"redirect"`
	Purpose  Purpose `json:"purpose"`
	UserID   int64   `json:"user_id,omitempty"`
	// NonceHash is the digest of the nonce the client that started the
	// round trip chose, when it chose one; the client presents the nonce
	// again when it redeems the state, so a state an attacker started cannot
	// be completed by a victim's browser. The nonce itself is never stored.
	NonceHash string `json:"nonce_hash,omitempty"`
}

var consumeScript = redis.NewScript(`
local value = redis.call("GET", KEYS[1])
if not value then
  return false
end
redis.call("DEL", KEYS[1])
return value
`)

// key is where the state of a round trip through method is stored.
func key(method, state string) string { return method + ":" + state }

// Issue creates the state of a round trip through method, in scope, that
// comes back to redirect, and returns it for the authorization URL. A
// non-empty nonce binds the state to the client that chose it: Consume then
// requires the same nonce.
func Issue(ctx context.Context, client *redis.Client, method string, scope Scope, redirect, nonce string) (string, error) {
	stored := record{Redirect: redirect, Purpose: scope.Purpose, UserID: scope.UserID}
	if nonce != "" {
		stored.NonceHash = hashNonce(nonce)
	}
	value, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	state := random.KeyNew(32, 1)
	if err := client.Set(ctx, key(method, state), value, TTL).Err(); err != nil {
		return "", err
	}
	return state, nil
}

// Consume redeems a state of method once and returns its redirect. The state
// is spent whatever the outcome; one issued in another scope than scope is
// refused with ErrScope, and one whose nonce binding does not match nonce
// with ErrNonce: a state issued with a nonce needs the same nonce back, and
// a state issued without one is refused when a nonce is presented, since the
// client that started that round trip was not the one presenting it. The Lua
// implementation keeps it atomic on Redis versions older than 6.2.
func Consume(ctx context.Context, client *redis.Client, method string, scope Scope, state, nonce string) (string, error) {
	value, err := consumeScript.Run(ctx, client, []string{key(method, state)}).Text()
	if errors.Is(err, redis.Nil) {
		return "", ErrUnknown
	}
	if err != nil {
		return "", err
	}
	stored, err := decode(value)
	if err != nil {
		return "", err
	}
	if stored.Purpose != scope.Purpose || stored.UserID != scope.UserID {
		return "", ErrScope
	}
	if !nonceMatches(stored.NonceHash, nonce) {
		return "", ErrNonce
	}
	return stored.Redirect, nil
}

// hashNonce is the stored form of a nonce.
func hashNonce(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}

// nonceMatches reports whether the nonce a client presents belongs to a
// state stored with storedHash: both absent, or the digest of the nonce
// equal to the stored one.
func nonceMatches(storedHash, nonce string) bool {
	if storedHash == "" {
		return nonce == ""
	}
	if nonce == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashNonce(nonce)), []byte(storedHash)) == 1
}

// Peek returns the redirect of a state of method without redeeming it: the
// Apple form-post callback hands the state on to the sign-in, which redeems
// it within its scope.
func Peek(ctx context.Context, client *redis.Client, method, state string) (string, error) {
	value, err := client.Get(ctx, key(method, state)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrUnknown
	}
	if err != nil {
		return "", err
	}
	stored, err := decode(value)
	if err != nil {
		return "", err
	}
	return stored.Redirect, nil
}

// decode reads a stored state. A value that is not a record was stored by
// an earlier version without a scope; it is unknown rather than trusted.
func decode(value string) (record, error) {
	var stored record
	if err := json.Unmarshal([]byte(value), &stored); err != nil || stored.Purpose == "" {
		return record{}, ErrUnknown
	}
	return stored, nil
}
