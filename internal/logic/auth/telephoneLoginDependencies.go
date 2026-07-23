package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// TelephoneLoginPolicy contains the authentication-method policy required by
// telephone login.
type TelephoneLoginPolicy interface {
	EnsureMethodEnabled(ctx context.Context, method string) error
}

// TelephoneLoginStore is the persistence surface used by telephone login.
// It excludes unrelated application repositories.
type TelephoneLoginStore interface {
	User() repository.UserRepo
	UserAuth() repository.UserAuthRepo
	Log() repository.LogRepo
}

// TelephoneLoginConfig is the configuration snapshot consumed by telephone
// login.
type TelephoneLoginConfig struct {
	JWTAccessSecret string
	JWTAccessExpire int64
}

// TelephoneLoginDependencies explicitly declares the collaborators of
// telephone login instead of passing ServiceContext to business logic.
type TelephoneLoginDependencies struct {
	Store        TelephoneLoginStore
	Redis        *redis.Client
	Config       TelephoneLoginConfig
	Policy       TelephoneLoginPolicy
	DeviceBinder DeviceBinder
}
