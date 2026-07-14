package oauth

import (
	"net/http"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
)

func Test_appleLoginRedirect_preserves_found_location_when_state_is_valid(t *testing.T) {
	// Given
	req := &dto.AppleLoginCallbackRequest{Code: "code value", State: "state value"}

	// When
	redirect := appleLoginRedirect("https://panel.example/callback", req, http.StatusFound)

	// Then
	if redirect.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusFound)
	}
	if redirect.Location != "https://panel.example/callback?code=code+value&method=apple&state=state+value" {
		t.Fatalf("location = %q", redirect.Location)
	}
}

func Test_appleLoginRedirect_encodes_query_components_when_state_or_code_have_delimiters(t *testing.T) {
	// Given
	req := &dto.AppleLoginCallbackRequest{Code: "code with spaces&symbols=1&2", State: "state?x=1&y=2"}

	// When
	redirect := appleLoginRedirect("https://panel.example/callback?from=apple", req, http.StatusFound)

	// Then
	if redirect.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusFound)
	}
	if redirect.Location != "https://panel.example/callback?code=code+with+spaces%26symbols%3D1%262&from=apple&method=apple&state=state%3Fx%3D1%26y%3D2" {
		t.Fatalf("location = %q", redirect.Location)
	}
}

func Test_appleLoginRedirect_preserves_temporary_redirect_when_state_is_invalid(t *testing.T) {
	// Given
	req := &dto.AppleLoginCallbackRequest{Code: "ignored", State: "ignored"}

	// When
	redirect := appleLoginRedirect("https://panel.example", req, http.StatusTemporaryRedirect)

	// Then
	if redirect.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusTemporaryRedirect)
	}
	if redirect.Location != "https://panel.example" {
		t.Fatalf("location = %q", redirect.Location)
	}
}
