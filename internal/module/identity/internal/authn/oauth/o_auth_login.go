package oauth

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
)

// OAuthLogin returns the URL that starts a sign-in through req.Method. The
// nonce the client chose, if any, binds the sign-in to that client:
// OAuthLoginGetToken completes it only with the same nonce.
func (s *Service) OAuthLogin(ctx context.Context, req *dto.OAuthLoginRequest) (*dto.OAuthLoginResponse, error) {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, req.Method); err != nil {
		return nil, err
	}
	uri, err := s.deps.Flow.AuthURL(ctx, oauthstate.LoginScope(), req.Method, req.Redirect, req.Nonce)
	if err != nil {
		return nil, err
	}
	return &dto.OAuthLoginResponse{Redirect: uri}, nil
}
