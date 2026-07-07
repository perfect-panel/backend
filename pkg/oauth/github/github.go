package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/perfect-panel/server/pkg/logger"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Client struct {
	*oauth2.Config
}

// UserInfo represents the GitHub user information.
type UserInfo struct {
	OpenID  int64  `json:"id"`
	Login   string `json:"login"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Avatar  string `json:"avatar_url"`
	HTMLURL string `json:"html_url"`
}

// EmailInfo represents a GitHub email address.
type EmailInfo struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func New(config *Config) *Client {
	return &Client{
		&oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  config.RedirectURL,
			Scopes:       []string{"read:user", "user:email"},
			Endpoint:     github.Endpoint,
		},
	}
}

// GetUserInfo fetches the user profile from the GitHub API using the access token.
// If the email is not publicly visible on the profile, it falls back to the emails API.
func (c *Client) GetUserInfo(token string) (*UserInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := c.Config.Client(ctx, &oauth2.Token{AccessToken: token})

	// Fetch user profile
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		logger.Error("[GitHub OAuth 2.0] Get User Info", logger.Field("error", err.Error()))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("[GitHub OAuth 2.0] Get User Info unexpected status",
			logger.Field("status", resp.StatusCode))
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		logger.Error("[GitHub OAuth 2.0] Decode User Info", logger.Field("error", err.Error()))
		return nil, err
	}

	// If the user's email is not public, fetch it from the emails endpoint
	if userInfo.Email == "" {
		email, err := c.GetPrimaryEmail(token)
		if err != nil {
			logger.Error("[GitHub OAuth 2.0] Get Primary Email", logger.Field("error", err.Error()))
		} else {
			userInfo.Email = email
		}
	}

	return &userInfo, nil
}

// GetPrimaryEmail fetches the primary verified email from the GitHub emails API.
func (c *Client) GetPrimaryEmail(token string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := c.Config.Client(ctx, &oauth2.Token{AccessToken: token})

	resp, err := client.Get("https://api.github.com/user/emails")
	if err != nil {
		logger.Error("[GitHub OAuth 2.0] Get Emails", logger.Field("error", err.Error()))
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github emails api returned status %d", resp.StatusCode)
	}

	var emails []EmailInfo
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		logger.Error("[GitHub OAuth 2.0] Decode Emails", logger.Field("error", err.Error()))
		return "", err
	}

	// Prefer primary + verified
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	// Fallback: return the first verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	return "", fmt.Errorf("no verified email found")
}
