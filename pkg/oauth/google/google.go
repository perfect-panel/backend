package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/perfect-panel/server/pkg/logger"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}
type Client struct {
	*oauth2.Config
}
type UserInfo struct {
	OpenID        string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	VerifiedEmail bool   `json:"verified_email"`
}

func New(config *Config) *Client {
	return &Client{
		&oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  config.RedirectURL,
			Scopes:       []string{"openid", "profile", "email"},
			Endpoint:     google.Endpoint,
		},
	}
}

func (c *Client) GetUserInfo(token string) (*UserInfo, error) {
	client := c.Config.Client(context.Background(), &oauth2.Token{AccessToken: token})
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		logger.Error("[Google OAuth 2.0] Get User Info", logger.Field("error", err.Error()))
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("[Google OAuth 2.0] Read response body", logger.Field("error", err.Error()))
		return nil, err
	}

	var raw struct {
		ID            string      `json:"id"`
		Email         string      `json:"email"`
		Name          string      `json:"name"`
		Picture       string      `json:"picture"`
		VerifiedEmail interface{} `json:"verified_email"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		logger.Error("[Google OAuth 2.0] Decode User Info", logger.Field("error", err.Error()))
		return nil, err
	}

	verified := false
	switch v := raw.VerifiedEmail.(type) {
	case bool:
		verified = v
	case string:
		verified = v == "true"
	}

	return &UserInfo{
		OpenID:        raw.ID,
		Email:         raw.Email,
		Name:          raw.Name,
		Picture:       raw.Picture,
		VerifiedEmail: verified,
	}, nil
}

// parseInt64 safely converts an interface{} to int64, handling the common
// string/number variations that can come from JSON.
func parseInt64(v interface{}) (int64, error) {
	switch val := v.(type) {
	case float64:
		return int64(val), nil
	case string:
		var n int64
		if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
			return 0, fmt.Errorf("cannot parse %q as int64", val)
		}
		return n, nil
	case json.Number:
		return val.Int64()
	default:
		return 0, fmt.Errorf("unexpected type %T for int64 value", v)
	}
}
