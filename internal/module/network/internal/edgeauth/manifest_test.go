package edgeauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestVerifyManifestRequest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cfg := config.EdgeSubscribeConfig{
		Enabled: true,
		Keys:    []config.EdgeSubscribeAccessKey{{ID: "edge-a", Secret: "test-secret"}},
	}
	token := "subscription-token"
	timestamp := "1700000000"
	requestID := "f0a8cb72-7c5b-4df5-9935-3f0e67eac79e"
	header := signedHeader("edge-a", "test-secret", token, timestamp, requestID)

	if _, ok := AuthenticateManifestRequest(header, token, requestID, cfg, now); !ok {
		t.Fatal("expected valid edge request to be accepted")
	}
	if _, ok := AuthenticateManifestRequest(header, "other-token", requestID, cfg, now); ok {
		t.Fatal("signature must be bound to the token")
	}
	if _, ok := AuthenticateManifestRequest(header, token, requestID, cfg, now.Add(301*time.Second)); ok {
		t.Fatal("expired signature must be rejected")
	}
	if _, ok := AuthenticateManifestRequest(header, token, "3fdf6c25-9d7d-4dcc-b9d0-4748c9cabb64", cfg, now); ok {
		t.Fatal("signature must be bound to the request ID")
	}
	if _, ok := AuthenticateManifestRequest("PPanel-Edge-HMAC kid=edge-a, ts=1700000000, sig=not-hex", token, requestID, cfg, now); ok {
		t.Fatal("malformed signature must be rejected")
	}
}

func TestClaimManifestRequestRejectsReplay(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := config.EdgeSubscribeConfig{MaxClockSkewSeconds: 60}
	requestID := "f0a8cb72-7c5b-4df5-9935-3f0e67eac79e"
	claimed, err := ClaimManifestRequest(t.Context(), client, "edge-a", requestID, cfg)
	if err != nil || !claimed {
		t.Fatalf("expected first request claim to succeed, claimed=%v err=%v", claimed, err)
	}
	claimed, err = ClaimManifestRequest(t.Context(), client, "edge-a", requestID, cfg)
	if err != nil || claimed {
		t.Fatalf("expected replay request claim to fail, claimed=%v err=%v", claimed, err)
	}
}

func signedHeader(kid, secret, token, timestamp, requestID string) string {
	tokenHash := sha256.Sum256([]byte(token))
	requestIDHash := sha256.Sum256([]byte(requestID))
	canonical := "v2\nGET\n/api/edge/v1/manifest\n" + hex.EncodeToString(tokenHash[:]) + "\n" + timestamp + "\n" + hex.EncodeToString(requestIDHash[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return "PPanel-Edge-HMAC kid=" + kid + ", ts=" + timestamp + ", sig=" + hex.EncodeToString(mac.Sum(nil))
}
