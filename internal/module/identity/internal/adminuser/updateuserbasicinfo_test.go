package adminuser

import (
	"testing"

	"errors"

	"github.com/perfect-panel/backend/pkg/xerr"
)

const validAvatar = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestValidateAvatarUpdate(t *testing.T) {
	remoteAvatar := "https://lh3.googleusercontent.com/a/example"

	tests := []struct {
		name      string
		current   string
		requested string
		wantCode  uint32
	}{
		{
			name:      "retains existing OAuth avatar URL",
			current:   remoteAvatar,
			requested: remoteAvatar,
		},
		{
			name:      "clears existing OAuth avatar URL",
			current:   remoteAvatar,
			requested: "",
		},
		{
			name:      "replaces existing OAuth avatar URL with valid Base64 image",
			current:   remoteAvatar,
			requested: validAvatar,
		},
		{
			name:      "rejects a new remote avatar URL",
			current:   remoteAvatar,
			requested: "https://example.com/avatar.png",
			wantCode:  xerr.InvalidParams,
		},
		{
			name:      "rejects invalid Base64 image",
			current:   remoteAvatar,
			requested: "not-base64",
			wantCode:  xerr.InvalidParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAvatarUpdate(tt.current, tt.requested)
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("validateAvatarUpdate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatal("validateAvatarUpdate() error = nil, want validation error")
			}

			var codeErr *xerr.CodeError
			if !errors.As(err, &codeErr) {
				t.Fatalf("error = %T, want wrapped *xerr.CodeError", err)
			}
			if codeErr.GetErrCode() != tt.wantCode {
				t.Fatalf("error code = %d, want %d", codeErr.GetErrCode(), tt.wantCode)
			}
		})
	}
}
