package profile

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// BindOAuth returns the URL that starts binding a req.Method identity to the
// calling account. The state it issues completes only this account's binding
// callback.
func (s *Service) BindOAuth(ctx context.Context, req *dto.BindOAuthRequest) (*dto.BindOAuthResponse, error) {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, req.Method); err != nil {
		return nil, err
	}
	current, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	// Binding runs in a signed-in session, whose state is already tied to
	// the account; it carries no client nonce.
	uri, err := s.deps.OAuth.AuthURL(ctx, oauthstate.BindScope(current.Id), req.Method, req.Redirect, "")
	if err != nil {
		return nil, err
	}
	return &dto.BindOAuthResponse{Redirect: uri}, nil
}
