package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// EmailLoginPolicy contains the authentication-method policy required by
// email login.
type EmailLoginPolicy interface {
	EnsureMethodEnabled(ctx context.Context, method string) error
}

// UserLoginStore is the persistence surface used by email login. It excludes
// unrelated application repositories.
type UserLoginStore interface {
	User() repository.UserRepo
	Log() repository.LogRepo
}

// UserLoginConfig is the configuration snapshot consumed by email login.
type UserLoginConfig struct {
	JWTAccessSecret string
	JWTAccessExpire int64
}

// UserLoginDependencies explicitly declares the collaborators of email login
// instead of passing ServiceContext to business logic.
type UserLoginDependencies struct {
	Store        UserLoginStore
	Redis        *redis.Client
	Config       UserLoginConfig
	Policy       EmailLoginPolicy
	DeviceBinder DeviceBinder
}
