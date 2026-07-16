package authmethod

import (
	"errors"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		list    string
		enforce bool
		want    string
		wantErr error
	}{
		{name: "canonical", email: " Alice@Example.COM ", want: "alice@example.com"},
		{name: "exact domain", email: "alice@example.com", list: "example.com", enforce: true, want: "alice@example.com"},
		{name: "subdomain", email: "alice@staff.example.com", list: "@example.com", enforce: true, want: "alice@staff.example.com"},
		{name: "json list", email: "alice@example.org", list: `["example.com", "example.org"]`, enforce: true, want: "alice@example.org"},
		{name: "suffix confusion", email: "alice@evil-example.com", list: "example.com", enforce: true, wantErr: ErrEmailDomainDenied},
		{name: "empty allowlist fails closed", email: "alice@example.com", enforce: true, wantErr: ErrEmailDomainListNil},
		{name: "display name rejected", email: "Alice <alice@example.com>", wantErr: ErrInvalidEmail},
		{name: "not an email", email: "not-an-email", wantErr: ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateEmail(tt.email, tt.list, tt.enforce)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateEmail() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ValidateEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}
