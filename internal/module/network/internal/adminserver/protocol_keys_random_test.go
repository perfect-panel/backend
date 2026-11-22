package adminserver

import (
	"encoding/hex"
	"testing"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// Generated server keys come from 32 random bytes each, in the format the
// nodes expect: 32 lowercase hex characters, all distinct.
func TestGeneratedServerKeysAreRandomHex(t *testing.T) {
	seen := make(map[string]bool)
	for range 64 {
		protocol := node.Protocol{Type: "shadowsocksr"}
		ensureGeneratedProtocolKey(&protocol, nil)
		if len(protocol.ServerKey) != generatedServerKeyLength {
			t.Fatalf("ServerKey length = %d, want %d", len(protocol.ServerKey), generatedServerKeyLength)
		}
		if _, err := hex.DecodeString(protocol.ServerKey); err != nil {
			t.Fatalf("ServerKey %q is not hex: %v", protocol.ServerKey, err)
		}
		if seen[protocol.ServerKey] {
			t.Fatalf("ServerKey %q was generated twice", protocol.ServerKey)
		}
		seen[protocol.ServerKey] = true
	}

	first, second := randomServerKeySeed(), randomServerKeySeed()
	if first == second {
		t.Fatal("two seeds are the same")
	}
	for _, seed := range []string{first, second} {
		if raw, err := hex.DecodeString(seed); err != nil || len(raw) != generatedServerKeySeedBytes {
			t.Fatalf("seed %q decodes to %d bytes, %v; want %d", seed, len(raw), err, generatedServerKeySeedBytes)
		}
	}
}
