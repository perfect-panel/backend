package user

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// OAuthMethodPolicy checks whether a requested OAuth method is enabled.
type OAuthMethodPolicy interface {
	EnsureMethodEnabled(ctx context.Context, method string) error
}

// BindOAuthCallbackDependencies contains the collaborators required to bind an
// OAuth identity to the authenticated user. It deliberately excludes
// ServiceContext and unrelated repositories.
type BindOAuthCallbackDependencies struct {
	Auth      repository.AuthRepo
	UserAuth  repository.UserAuthRepo
	UserCache repository.UserCacheRepo
	Redis     *redis.Client
	Policy    OAuthMethodPolicy
}
