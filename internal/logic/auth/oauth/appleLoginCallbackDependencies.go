package oauth

import "github.com/redis/go-redis/v9"

// AppleLoginCallbackDependencies explicitly declares the state store and
// fallback redirect used by the Apple OAuth callback instead of passing
// ServiceContext to business logic.
type AppleLoginCallbackDependencies struct {
	Redis            *redis.Client
	FallbackRedirect string
}
