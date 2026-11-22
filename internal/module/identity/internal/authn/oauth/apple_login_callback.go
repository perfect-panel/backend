package oauth

import (
	"context"
	"net/http"
	"net/url"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/logger"
)

// AppleLoginRedirect is the redirect that answers Apple's form post.
type AppleLoginRedirect struct {
	StatusCode int
	Location   string
}

// fallbackLocation is where the browser is sent when the callback cannot be
// completed: the configured site host, or the root of the host the callback
// was posted to when none is configured. It never carries the callback.
const fallbackLocation = "/"

// AppleLoginCallback answers Apple's form post: it sends the browser to the
// redirect the state was issued for, carrying the code and state on to the
// sign-in, which redeems them. An unknown state, or a stored redirect off the
// pinned host (the site host, or the host Apple posted to when none is
// configured), sends the browser to the fallback instead, without the code:
// the code signs its owner in, so it goes only where the sign-in was started.
func (s *Service) AppleLoginCallback(ctx context.Context, req *dto.AppleLoginCallbackRequest) (*AppleLoginRedirect, error) {
	siteHost := s.deps.Config().SiteHost
	fallback := siteHost
	if fallback == "" {
		fallback = fallbackLocation
	}
	log := logger.WithContext(ctx)
	stored, err := oauthstate.Peek(ctx, s.deps.Redis, "apple", req.State)
	if err != nil {
		log.Errorw("get apple state code from redis failed", logger.Field("error", err.Error()), logger.Field("code", req.State))
		return appleLoginRedirect(fallback, req, http.StatusTemporaryRedirect), nil
	}
	// Never 302 off the pinned host, even if a hostile redirect slipped into
	// the state store; without any host to pin to, never 302 at all.
	if err := oauthstate.ValidateRedirect(stored, oauthstate.ResolvePin(ctx, siteHost)); err != nil {
		log.Errorw("stored apple redirect rejected", logger.Field("error", err.Error()), logger.Field("redirect", stored))
		return appleLoginRedirect(fallback, req, http.StatusTemporaryRedirect), nil
	}
	redirect := appleLoginRedirect(stored, req, http.StatusFound)
	// The location carries Apple's authorization code and the state, which
	// together sign the user in; only where the browser is sent is logged.
	log.Infow("redirect to apple login page", redirectFields(redirect.Location)...)
	return redirect, nil
}

// redirectFields names where a redirect sends the browser, its host and
// path, leaving out the query that carries the credential.
func redirectFields(location string) []logger.LogField {
	parsed, err := url.Parse(location)
	if err != nil {
		return []logger.LogField{logger.Field("host", "<unparsable>")}
	}
	return []logger.LogField{logger.Field("host", parsed.Host), logger.Field("path", parsed.Path)}
}

func appleLoginRedirect(location string, req *dto.AppleLoginCallbackRequest, statusCode int) *AppleLoginRedirect {
	if statusCode == http.StatusTemporaryRedirect {
		return &AppleLoginRedirect{StatusCode: statusCode, Location: location}
	}

	parsedLocation, err := url.Parse(location)
	if err != nil {
		return &AppleLoginRedirect{StatusCode: statusCode, Location: location}
	}

	query := parsedLocation.Query()
	query.Set("method", "apple")
	query.Set("code", req.Code)
	query.Set("state", req.State)
	parsedLocation.RawQuery = query.Encode()

	return &AppleLoginRedirect{
		StatusCode: statusCode,
		Location:   parsedLocation.String(),
	}
}
