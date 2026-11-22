package validation

import (
	"testing"

	identity "github.com/perfect-panel/backend/internal/module/identity/contract"
)

// Facebook signs in through the same OAuth flow as the other providers, so
// binding it must pass request validation too.
func TestOAuthBindRequestsAcceptEveryProvider(t *testing.T) {
	for _, method := range []string{"google", "apple", "telegram", "github", "facebook"} {
		if err := Validate(identity.BindOAuthRequest{Method: method, Redirect: "https://example.com"}); err != nil {
			t.Errorf("bind %s rejected: %v", method, err)
		}
		if err := Validate(identity.BindOAuthCallbackRequest{Method: method, Callback: map[string]any{"code": "x"}}); err != nil {
			t.Errorf("bind callback %s rejected: %v", method, err)
		}
	}
	if err := Validate(identity.BindOAuthRequest{Method: "myspace", Redirect: "https://example.com"}); err == nil {
		t.Error("an unknown provider passed validation")
	}
}
