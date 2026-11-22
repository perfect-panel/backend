// Package oauthflow runs the provider round trip OAuth sign-in and account
// binding share: the authorization URL with its state, and the callback that
// redeems the state and turns into the identity the provider vouches for.
// What happens with that identity (signing in, registering, binding) is up
// to the caller.
package oauthflow

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// providerTimeout bounds the provider requests of one callback.
const providerTimeout = 10 * time.Second

// replayGrace lets a client re-submit a single-use callback after a
// timed-out exchange; beyond it, a repeat is a replay.
const replayGrace = 60 * time.Second

// AuthConfigs loads a method's stored configuration.
type AuthConfigs interface {
	FindOneByMethod(ctx context.Context, method string) (*auth.Auth, error)
}

// Deps declares the flow's collaborators.
type Deps struct {
	Auths AuthConfigs
	Redis *redis.Client
	// Providers lists the methods; oauthprovider.Default when nil.
	Providers oauthprovider.Registry
	// SiteHost snapshots the configured site host redirects are pinned to;
	// optional.
	SiteHost func() string
}

// Flow runs OAuth round trips.
type Flow struct {
	deps Deps
}

func New(deps Deps) *Flow {
	if deps.Providers == nil {
		deps.Providers = oauthprovider.Default()
	}
	return &Flow{deps: deps}
}

// AuthURL returns the URL starting a round trip through method, in scope,
// that comes back to redirect. The state it issues is redeemed only by a
// callback of the same scope: a sign-in's state cannot complete a binding,
// and a binding's state completes only for the account it was issued for.
// A non-empty nonce, chosen by the client, binds the state to that client:
// the callback must present it again (Identify).
//
// A method that sends the browser to redirect itself (Apple's form post,
// Telegram's widget) has its redirect pinned to the site host, or to the
// request's own host while no site host is configured; without either the
// sign-in is refused, since an unpinned redirect would hand a victim's
// callback to whoever started the round trip.
func (f *Flow) AuthURL(ctx context.Context, scope oauthstate.Scope, method, redirect, nonce string) (string, error) {
	spec, provider, err := f.provider(ctx, method)
	if err != nil {
		return "", err
	}
	if spec.PinRedirect {
		if err := oauthstate.ValidateRedirect(redirect, oauthstate.ResolvePin(ctx, f.siteHost())); err != nil {
			if errors.Is(err, oauthstate.ErrUnpinned) {
				logger.WithContext(ctx).Errorw("oauth sign-in refused: no site host to pin its redirect to",
					logger.Field("method", method), logger.Field("error", err.Error()))
				return "", xerr.Wrapf(err, xerr.OAuthProviderMisconfigured, "%s sign-in needs the site host configured", method)
			}
			return "", xerr.Wrapf(err, xerr.InvalidParams, "invalid %s redirect", method)
		}
	}
	state := ""
	if spec.State {
		if state, err = oauthstate.Issue(ctx, f.deps.Redis, method, scope, redirect, nonce); err != nil {
			return "", xerr.Wrapf(err, xerr.ERROR, "store %s state", method)
		}
	}
	uri, err := provider.AuthURL(redirect, state)
	if err != nil {
		return "", codeFor(ctx, method, err)
	}
	return uri, nil
}

// Identify completes the round trip of a callback through method, in scope,
// and returns the identity the provider vouches for. A state-based callback
// redeems its state, which must have been issued in the same scope and, when
// it was issued with a nonce, with the same nonce; a single-use one
// (Telegram) is redeemed here too.
func (f *Flow) Identify(ctx context.Context, scope oauthstate.Scope, method string, fields map[string]any, nonce string) (*oauthprovider.Identity, error) {
	spec, ok := f.deps.Providers.Lookup(method)
	if !ok {
		return nil, notSupported(method)
	}
	callback := oauthprovider.Callback{Fields: fields}
	if spec.State {
		code, _ := fields["code"].(string)
		state, _ := fields["state"].(string)
		if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
			return nil, xerr.Errorf(xerr.InvalidParams, "%s callback needs a code and a state", method)
		}
		redirect, err := oauthstate.Consume(ctx, f.deps.Redis, method, scope, state, nonce)
		if err != nil {
			if errors.Is(err, oauthstate.ErrUnknown) || errors.Is(err, oauthstate.ErrScope) || errors.Is(err, oauthstate.ErrNonce) {
				return nil, xerr.Wrapf(err, xerr.OAuthStateInvalid, "redeem %s state", method)
			}
			return nil, xerr.Wrapf(err, xerr.ERROR, "redeem %s state", method)
		}
		callback.Code, callback.Redirect = code, redirect
	}
	_, provider, err := f.provider(ctx, method)
	if err != nil {
		return nil, err
	}
	providerCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	identity, err := provider.Identify(providerCtx, callback)
	if err != nil {
		return nil, codeFor(ctx, method, err)
	}
	if identity.Subject == "" {
		return nil, xerr.Errorf(xerr.OAuthProviderError, "%s returned no user id", method)
	}
	if identity.ReplayKey != "" {
		if err := f.redeem(ctx, method, identity.ReplayKey); err != nil {
			return nil, err
		}
	}
	return identity, nil
}

// redeem enforces single use of a callback without a state. The check fails
// closed: while Redis is unavailable a signed callback cannot be told from a
// replay of one, so the sign-in is refused rather than accepted on the
// callback's signature and freshness alone.
func (f *Flow) redeem(ctx context.Context, method, key string) error {
	allowed, err := oauthstate.ClaimSingleUse(ctx, f.deps.Redis, key, timeutil.Now(), replayGrace, oauthprovider.CallbackLifetime)
	if err != nil {
		logger.WithContext(ctx).Errorw("oauth callback replay check unavailable; refusing the callback",
			logger.Field("method", method), logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.ERROR, "%s callback replay check unavailable", method)
	}
	if !allowed {
		return xerr.Errorf(xerr.OAuthCallbackReplayed, "%s callback has already been used", method)
	}
	return nil
}

func (f *Flow) provider(ctx context.Context, method string) (oauthprovider.Method, oauthprovider.Provider, error) {
	spec, ok := f.deps.Providers.Lookup(method)
	if !ok {
		return spec, nil, notSupported(method)
	}
	stored, err := f.deps.Auths.FindOneByMethod(ctx, method)
	if err != nil {
		return spec, nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "load %s configuration", method)
	}
	provider, err := spec.New(stored.Config)
	if err != nil {
		return spec, nil, codeFor(ctx, method, err)
	}
	return spec, provider, nil
}

func (f *Flow) siteHost() string {
	if f.deps.SiteHost == nil {
		return ""
	}
	return f.deps.SiteHost()
}

func notSupported(method string) error {
	return xerr.Errorf(xerr.AuthenticatorNotSupportedError, "oauth method %q is not supported", method)
}

// codeFor attaches the client code of a provider failure. Failures that are
// not the client's are logged here: their codes are specific, so the access
// log does not record their cause.
func codeFor(ctx context.Context, method string, err error) error {
	code := xerr.OAuthProviderError
	switch {
	case errors.Is(err, oauthprovider.ErrIncompleteCallback):
		code = xerr.InvalidParams
	case errors.Is(err, oauthprovider.ErrInvalidCallback):
		code = xerr.OAuthCallbackInvalid
	case errors.Is(err, oauthprovider.ErrExpiredCallback):
		code = xerr.OAuthCallbackExpired
	case errors.Is(err, oauthprovider.ErrMisconfigured):
		code = xerr.OAuthProviderMisconfigured
	}
	if code == xerr.OAuthProviderError || code == xerr.OAuthProviderMisconfigured {
		logger.WithContext(ctx).Errorw("oauth provider failed", logger.Field("method", method), logger.Field("error", err.Error()))
	}
	return xerr.Wrapf(err, code, "%s sign-in", method)
}
