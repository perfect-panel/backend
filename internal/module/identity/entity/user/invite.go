package user

import (
	"time"

	"github.com/perfect-panel/backend/pkg/random"
)

// GenerateInviteCode preserves the existing user invitation-code format.
func GenerateInviteCode(id int64) string {
	return "u" + random.EncodeBase62(id+time.Now().UnixMilli())
}
