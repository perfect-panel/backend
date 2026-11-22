package bootstrap

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
)

// The admin's verification-code settings are stored under VerifyCode-prefixed
// keys; they must reach the runtime config instead of falling back to the
// hard-coded defaults.
func TestVerifyCodeFromSettingsReadsStoredKeys(t *testing.T) {
	got := verifyCodeFromSettings(context.Background(), []*system.System{
		{Category: "verify_code", Key: "VerifyCodeExpireTime", Value: "600", Type: "int"},
		{Category: "verify_code", Key: "VerifyCodeLimit", Value: "5", Type: "int"},
		{Category: "verify_code", Key: "VerifyCodeInterval", Value: "120", Type: "int"},
	})

	if got.ExpireTime != 600 || got.Limit != 5 || got.Interval != 120 {
		t.Fatalf("verify code config = %+v, want ExpireTime 600, Limit 5, Interval 120", got)
	}
}
