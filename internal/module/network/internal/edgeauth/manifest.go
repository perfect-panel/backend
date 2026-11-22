// Package edgeauth authenticates Cloudflare edge-subscribe Workers. It is kept
// separate from user and admin authentication so an edge credential cannot be
// used against any other PPanel API.
package edgeauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"uuid"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

const (
	manifestPath      = "/api/edge/v1/manifest"
	signatureVersion  = "v2"
	defaultClockSkew  = int64(300)
	maximumClockSkew  = int64(300)
	replayCachePrefix = "edge:manifest:request:"
)

// AuthenticateManifestRequest checks the HMAC format emitted by
// edge-subscribe. The signed bytes are:
// v2\nGET\n/api/edge/v1/manifest\nSHA256(token)\nUnixTimestamp\nSHA256(requestID).
// The returned key ID is safe to use as a replay-cache partition key.
func AuthenticateManifestRequest(authorization, token, requestID string, cfg config.EdgeSubscribeConfig, now time.Time) (string, bool) {
	if !cfg.Enabled || token == "" || !validRequestID(requestID) {
		return "", false
	}
	credential, ok := parseAuthorization(authorization)
	if !ok {
		return "", false
	}
	key, ok := findKey(cfg.Keys, credential.kid)
	if !ok || key.Secret == "" {
		return "", false
	}
	timestamp, err := strconv.ParseInt(credential.timestamp, 10, 64)
	if err != nil {
		return "", false
	}
	skew := clockSkew(cfg)
	if timestamp < now.Unix()-skew || timestamp > now.Unix()+skew {
		return "", false
	}
	tokenHash := sha256.Sum256([]byte(token))
	requestIDHash := sha256.Sum256([]byte(requestID))
	canonical := strings.Join([]string{signatureVersion, "GET", manifestPath, hex.EncodeToString(tokenHash[:]), credential.timestamp, hex.EncodeToString(requestIDHash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(key.Secret))
	_, _ = mac.Write([]byte(canonical))
	expected := mac.Sum(nil)
	provided, err := hex.DecodeString(credential.signature)
	if err != nil || len(provided) != sha256.Size {
		return "", false
	}
	return credential.kid, subtle.ConstantTimeCompare(expected, provided) == 1
}

// ClaimManifestRequest records a valid request ID for the entire time window
// in which its signature could be accepted. A replay is rejected. Redis errors
// are returned to the caller so the endpoint can fail closed.
func ClaimManifestRequest(ctx context.Context, client redis.Cmdable, kid, requestID string, cfg config.EdgeSubscribeConfig) (bool, error) {
	if client == nil {
		return false, errors.New("edge replay cache is unavailable")
	}
	requestIDHash := sha256.Sum256([]byte(requestID))
	key := replayCachePrefix + kid + ":" + hex.EncodeToString(requestIDHash[:])
	return client.SetNX(ctx, key, "1", replayTTL(cfg)).Result()
}

func clockSkew(cfg config.EdgeSubscribeConfig) int64 {
	skew := cfg.MaxClockSkewSeconds
	if skew <= 0 {
		return defaultClockSkew
	}
	if skew > maximumClockSkew {
		return maximumClockSkew
	}
	return skew
}

func replayTTL(cfg config.EdgeSubscribeConfig) time.Duration {
	return time.Duration(clockSkew(cfg)*2+1) * time.Second
}

func validRequestID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

type authorization struct {
	kid       string
	timestamp string
	signature string
}

func parseAuthorization(raw string) (authorization, bool) {
	const scheme = "PPanel-Edge-HMAC "
	if !strings.HasPrefix(raw, scheme) {
		return authorization{}, false
	}
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(raw, scheme)), ",")
	if len(parts) != 3 {
		return authorization{}, false
	}
	values := make(map[string]string, len(parts))
	for _, part := range parts {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || key == "" || value == "" {
			return authorization{}, false
		}
		if _, duplicate := values[key]; duplicate {
			return authorization{}, false
		}
		values[key] = value
	}
	if len(values) != 3 || values["kid"] == "" || values["ts"] == "" || values["sig"] == "" {
		return authorization{}, false
	}
	return authorization{kid: values["kid"], timestamp: values["ts"], signature: values["sig"]}, true
}

func findKey(keys []config.EdgeSubscribeAccessKey, kid string) (config.EdgeSubscribeAccessKey, bool) {
	for _, key := range keys {
		if key.ID == kid {
			return key, true
		}
	}
	return config.EdgeSubscribeAccessKey{}, false
}
