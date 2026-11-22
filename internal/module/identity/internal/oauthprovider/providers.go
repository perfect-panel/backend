package oauthprovider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider/apple"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider/facebook"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider/github"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider/google"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider/telegram"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"golang.org/x/oauth2"
)

// CallbackLifetime bounds how old a signed callback without a state round
// trip may be. The Telegram widget result is a bearer credential that
// reaches us through a URL fragment, so the window is the round trip a user
// needs, not the day it used to be.
const CallbackLifetime = 5 * time.Minute

// callbackClockSkew is how far ahead of our clock a signed callback may be.
const callbackClockSkew = 5 * time.Minute

// ErrIncompleteCallback reports a callback missing a field the method needs.
var ErrIncompleteCallback = errors.New("oauth callback is incomplete")

func misconfigured(err error) error { return fmt.Errorf("%w: %w", ErrMisconfigured, err) }
func rejected(err error) error      { return fmt.Errorf("%w: %w", ErrRejected, err) }

// codeProvider is a provider of the OAuth 2.0 authorization code flow.
type codeProvider struct {
	oauth2    func(redirect string) *oauth2.Config
	urlOption []oauth2.AuthCodeOption
	identify  func(ctx context.Context, token string) (*Identity, error)
}

func (p *codeProvider) AuthURL(redirect, state string) (string, error) {
	return p.oauth2(redirect).AuthCodeURL(state, p.urlOption...), nil
}

func (p *codeProvider) Identify(ctx context.Context, callback Callback) (*Identity, error) {
	token, err := p.oauth2(callback.Redirect).Exchange(ctx, callback.Code)
	if err != nil {
		return nil, rejected(fmt.Errorf("exchange authorization code: %w", err))
	}
	identity, err := p.identify(ctx, token.AccessToken)
	if err != nil {
		return nil, rejected(err)
	}
	return identity, nil
}

func newGoogle(stored string) (Provider, error) {
	var cfg auth.GoogleAuthConfig
	if err := cfg.Unmarshal(stored); err != nil {
		return nil, misconfigured(err)
	}
	client := func(redirect string) *google.Client {
		return google.New(&google.Config{ClientID: cfg.ClientId, ClientSecret: cfg.ClientSecret, RedirectURL: redirect})
	}
	return &codeProvider{
		oauth2:    func(redirect string) *oauth2.Config { return client(redirect).Config },
		urlOption: []oauth2.AuthCodeOption{oauth2.AccessTypeOffline},
		identify: func(ctx context.Context, token string) (*Identity, error) {
			info, err := client("").GetUserInfo(ctx, token)
			if err != nil {
				return nil, err
			}
			identity := &Identity{Subject: info.OpenID, Avatar: info.Picture}
			if info.VerifiedEmail {
				identity.Email = info.Email
			}
			return identity, nil
		},
	}, nil
}

func newGithub(stored string) (Provider, error) {
	var cfg auth.GithubAuthConfig
	if err := cfg.Unmarshal(stored); err != nil {
		return nil, misconfigured(err)
	}
	client := func(redirect string) *github.Client {
		return github.New(&github.Config{ClientID: cfg.ClientId, ClientSecret: cfg.ClientSecret, RedirectURL: redirect})
	}
	return &codeProvider{
		oauth2:    func(redirect string) *oauth2.Config { return client(redirect).Config },
		urlOption: []oauth2.AuthCodeOption{oauth2.AccessTypeOffline},
		identify: func(ctx context.Context, token string) (*Identity, error) {
			info, err := client("").GetUserInfo(ctx, token)
			if err != nil {
				return nil, err
			}
			// GetUserInfo only reports a verified address.
			return &Identity{Subject: strconv.FormatInt(info.OpenID, 10), Email: info.Email, Avatar: info.Avatar}, nil
		},
	}, nil
}

func newFacebook(stored string) (Provider, error) {
	var cfg auth.FacebookAuthConfig
	if err := cfg.Unmarshal(stored); err != nil {
		return nil, misconfigured(err)
	}
	client := func(redirect string) *facebook.Client {
		return facebook.New(&facebook.Config{ClientID: cfg.ClientId, ClientSecret: cfg.ClientSecret, RedirectURL: redirect})
	}
	return &codeProvider{
		oauth2: func(redirect string) *oauth2.Config { return client(redirect).Config },
		identify: func(ctx context.Context, token string) (*Identity, error) {
			info, err := client("").GetUserInfo(ctx, token)
			if err != nil {
				return nil, err
			}
			// The Graph API only returns a confirmed address.
			return &Identity{Subject: info.OpenID, Email: info.Email, Avatar: info.Picture}, nil
		},
	}, nil
}

// appleProvider signs in with Apple's form-post flow: Apple posts the code
// to our callback, which redirects the browser to the stored redirect.
type appleProvider struct {
	cfg auth.AppleAuthConfig
}

func newApple(stored string) (Provider, error) {
	var cfg auth.AppleAuthConfig
	if err := cfg.Unmarshal(stored); err != nil {
		return nil, misconfigured(err)
	}
	return &appleProvider{cfg: cfg}, nil
}

func (p *appleProvider) AuthURL(_, state string) (string, error) {
	const uri = "https://appleid.apple.com/auth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s&scope=name email&response_mode=form_post"
	return fmt.Sprintf(uri, p.cfg.ClientId, p.cfg.RedirectURL+"/v1/auth/oauth/callback/apple", state), nil
}

func (p *appleProvider) Identify(ctx context.Context, callback Callback) (*Identity, error) {
	client, err := apple.New(apple.Config{
		ClientID:     p.cfg.ClientId,
		TeamID:       p.cfg.TeamID,
		KeyID:        p.cfg.KeyID,
		ClientSecret: p.cfg.ClientSecret,
		RedirectURI:  p.cfg.RedirectURL,
	})
	if err != nil {
		return nil, misconfigured(fmt.Errorf("apple client: %w", err))
	}
	resp, err := client.VerifyWebToken(ctx, callback.Code)
	if err != nil {
		return nil, rejected(fmt.Errorf("verify web token: %w", err))
	}
	if resp.Error != "" {
		return nil, rejected(fmt.Errorf("verify web token: %s", resp.Error))
	}
	subject, err := apple.GetUniqueID(resp.IDToken)
	if err != nil {
		return nil, rejected(fmt.Errorf("read apple id token: %w", err))
	}
	claims, err := apple.GetClaims(resp.IDToken)
	if err != nil {
		return nil, rejected(fmt.Errorf("read apple id token: %w", err))
	}
	identity := &Identity{Subject: subject}
	if email, ok := (*claims)["email"].(string); ok && claimBool((*claims)["email_verified"]) {
		identity.Email = email
	}
	return identity, nil
}

// claimBool reads a boolean claim, which Apple sends as a string.
func claimBool(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

// telegramProvider signs in with the Telegram Login widget. There is no
// state round trip: the widget result is authenticated by its HMAC signature
// and its auth_date, and the flow redeems it once.
type telegramProvider struct {
	cfg auth.TelegramAuthConfig
}

func newTelegram(stored string) (Provider, error) {
	var cfg auth.TelegramAuthConfig
	if err := cfg.Unmarshal(stored); err != nil {
		return nil, misconfigured(err)
	}
	return &telegramProvider{cfg: cfg}, nil
}

func (p *telegramProvider) AuthURL(redirect, _ string) (string, error) {
	uri, err := telegram.BuildTelegramOAuthURL(p.cfg.BotToken, redirect)
	if err != nil {
		return "", misconfigured(err)
	}
	return uri, nil
}

func (p *telegramProvider) Identify(_ context.Context, callback Callback) (*Identity, error) {
	encoded, _ := callback.Fields["tgAuthResult"].(string)
	if encoded == "" {
		return nil, fmt.Errorf("%w: tgAuthResult is missing", ErrIncompleteCallback)
	}
	data, err := telegram.ParseAndValidateBase64([]byte(encoded), p.cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCallback, err)
	}
	if data.Id == nil || data.AuthDate == nil {
		return nil, fmt.Errorf("%w: the identity fields are missing", ErrIncompleteCallback)
	}
	now := timeutil.Now().Unix()
	if *data.AuthDate > now+int64(callbackClockSkew.Seconds()) || now-*data.AuthDate > int64(CallbackLifetime.Seconds()) {
		return nil, fmt.Errorf("%w: auth_date %d", ErrExpiredCallback, *data.AuthDate)
	}
	identity := &Identity{
		Subject: strconv.FormatInt(*data.Id, 10),
		// The signature alone does not bind the result to one exchange.
		ReplayKey: config.TelegramCallbackKey + ":" + oauthstate.PayloadFingerprint(encoded),
	}
	if data.PhotoUrl != nil {
		identity.Avatar = *data.PhotoUrl
	}
	return identity, nil
}
