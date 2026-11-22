package user

import (
	"testing"
	"time"

	"github.com/perfect-panel/backend/pkg/random"
)

func TestGenerateInviteCodePreservesLegacyEncoding(t *testing.T) {
	const id = int64(42)
	before := time.Now().UnixMilli()
	got := GenerateInviteCode(id)
	after := time.Now().UnixMilli()
	for at := before; at <= after; at++ {
		if got == "u"+random.EncodeBase62(id+at) {
			return
		}
	}
	t.Fatalf("invitation code %q does not match the legacy format", got)
}
