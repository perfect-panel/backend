package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

func stubAPI(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	original := apiURL
	apiURL = server.URL
	t.Cleanup(func() { apiURL = original })
}

// Only a verified address becomes the account's email; the profile email
// carries no verification.
func TestGetUserInfoTakesTheVerifiedEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":583231,"login":"octocat","email":"unverified@example.com","avatar_url":"https://cdn.example/a.png"}`))
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"email":"secondary@example.com","verified":true},{"email":"primary@example.com","primary":true,"verified":true}]`))
	})
	stubAPI(t, mux)

	info, err := New(&Config{}).GetUserInfo(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("GetUserInfo error = %v", err)
	}
	if info.OpenID != 583231 || info.Email != "primary@example.com" || info.Avatar != "https://cdn.example/a.png" {
		t.Fatalf("info = %+v", info)
	}
}

// Without a reachable or verified address the user still signs in, just
// without an email.
func TestGetUserInfoWithoutVerifiedEmail(t *testing.T) {
	logtest.Discard(t)
	for name, emails := range map[string]http.HandlerFunc{
		"none verified": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"email":"x@example.com","primary":true}]`))
		},
		"emails refused": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
	} {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":583231,"email":"unverified@example.com"}`))
			})
			mux.HandleFunc("/user/emails", emails)
			stubAPI(t, mux)
			info, err := New(&Config{}).GetUserInfo(context.Background(), "access-token")
			if err != nil || info.Email != "" {
				t.Fatalf("GetUserInfo = %+v, %v; want no email", info, err)
			}
		})
	}
}

func TestGetUserInfoRejectsFailedProfile(t *testing.T) {
	for name, profile := range map[string]http.HandlerFunc{
		"refused": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"no id":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"login":"octocat"}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/user", profile)
			stubAPI(t, mux)
			if info, err := New(&Config{}).GetUserInfo(context.Background(), "access-token"); err == nil {
				t.Fatalf("GetUserInfo = %+v, want an error", info)
			}
		})
	}
}
