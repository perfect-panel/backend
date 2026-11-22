// Package github is the GitHub API client of the GitHub sign-in method: the
// profile of the signed-in user and their verified email address.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/perfect-panel/backend/pkg/logger"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// apiURL is a variable so tests can point the client at a stub server.
var apiURL = "https://api.github.com"

// errNoVerifiedEmail reports an account without a verified address.
var errNoVerifiedEmail = errors.New("no verified email found")

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

// GetUserInfo fetches the user profile from the GitHub API using the access
// token. The profile email carries no verification metadata, so the email
// returned is the primary verified one from the emails API, or empty. The
// requests are bound to ctx, which carries the caller's deadline.
func (c *Client) GetUserInfo(ctx context.Context, token string) (*UserInfo, error) {
	var userInfo UserInfo
	if err := c.get(ctx, token, "/user", &userInfo); err != nil {
		return nil, fmt.Errorf("github user: %w", err)
	}
	if userInfo.OpenID == 0 {
		return nil, errors.New("github user has no id")
	}
	// Without a verified address the account still signs in, it just gets
	// no email binding.
	userInfo.Email = ""
	email, err := c.GetPrimaryEmail(ctx, token)
	switch {
	case err == nil:
		userInfo.Email = email
	case errors.Is(err, errNoVerifiedEmail):
	default:
		logger.WithContext(ctx).Errorw("[GitHub OAuth 2.0] get verified email", logger.Field("error", err.Error()))
	}
	return &userInfo, nil
}

// GetPrimaryEmail fetches the primary verified email from the GitHub emails
// API, falling back to any verified one.
func (c *Client) GetPrimaryEmail(ctx context.Context, token string) (string, error) {
	var emails []EmailInfo
	if err := c.get(ctx, token, "/user/emails", &emails); err != nil {
		return "", fmt.Errorf("github emails: %w", err)
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}
	return "", errNoVerifiedEmail
}

func (c *Client) get(ctx context.Context, token, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.Client(ctx, &oauth2.Token{AccessToken: token}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(into)
}
