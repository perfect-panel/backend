package migrate

import "testing"

func TestCanonicalAdminEmail(t *testing.T) {
	email, err := canonicalAdminEmail(" Admin@Example.COM ")
	if err != nil || email != "admin@example.com" {
		t.Fatalf("canonical admin email = %q, %v", email, err)
	}

	if _, err := canonicalAdminEmail(" \t "); err == nil {
		t.Fatal("empty canonical admin email must be rejected")
	}
}
