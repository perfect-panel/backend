package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/auth/identifier"
)

// Cache key prefixes used across the user domain cache.
const (
	cacheUserIdPrefix           = "cache:user:id:"
	cacheUserStatePrefix        = "cache:user:state:"
	cacheUserEmailPrefix        = "cache:user:email:v2:"
	cacheUserDeviceNumberPrefix = "cache:user:device:number:"
	cacheUserDeviceIdPrefix     = "cache:user:device:id:"
)

// CacheKeyGenerator produces the set of cache keys that hold the model.
type CacheKeyGenerator interface {
	GetCacheKeys() []string
}

// CacheManager clears cache keys directly or via CacheKeyGenerator models.
type CacheManager interface {
	ClearCache(ctx context.Context, keys ...string) error
	ClearModelCache(ctx context.Context, models ...CacheKeyGenerator) error
}

func (u *User) GetCacheKeys() []string {
	if u == nil {
		return []string{}
	}
	keys := []string{
		fmt.Sprintf("%s%d", cacheUserIdPrefix, u.Id),
		fmt.Sprintf("%s%d", cacheUserStatePrefix, u.Id),
	}

	for _, auth := range u.AuthMethods {
		if auth.AuthType == identifier.Email {
			keys = append(keys, fmt.Sprintf("%s%s", cacheUserEmailPrefix, identifier.CanonicalEmail(auth.AuthIdentifier)))
			break
		}
	}
	return keys
}

func (d *Device) GetCacheKeys() []string {
	if d == nil {
		return []string{}
	}
	keys := []string{}

	if d.Id != 0 {
		keys = append(keys, fmt.Sprintf("%s%d", cacheUserDeviceIdPrefix, d.Id))
	}
	if d.Identifier != "" {
		keys = append(keys, fmt.Sprintf("%s%s", cacheUserDeviceNumberPrefix, d.Identifier))
	}
	return keys
}

func (a *AuthMethods) GetCacheKeys() []string {
	if a == nil {
		return []string{}
	}
	keys := []string{}

	if a.UserId != 0 {
		keys = append(keys, fmt.Sprintf("%s%d", cacheUserIdPrefix, a.UserId))
		keys = append(keys, fmt.Sprintf("%s%d", cacheUserStatePrefix, a.UserId))
	}
	if a.AuthType == identifier.Email && a.AuthIdentifier != "" {
		keys = append(keys, fmt.Sprintf("%s%s", cacheUserEmailPrefix, identifier.CanonicalEmail(a.AuthIdentifier)))
	}
	return keys
}
