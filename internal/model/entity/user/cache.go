package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/pkg/authmethod"
)

// Cache key prefixes used across the user domain cache.
const (
	cacheUserIdPrefix             = "cache:user:id:"
	cacheUserEmailPrefix          = "cache:user:email:v2:"
	cacheUserSubscribeTokenPrefix = "cache:user:subscribe:token:"
	cacheUserSubscribeUserPrefix  = "cache:user:subscribe:user:v3:"
	cacheUserSubscribeIdPrefix    = "cache:user:subscribe:id:"
	cacheUserDeviceNumberPrefix   = "cache:user:device:number:"
	cacheUserDeviceIdPrefix       = "cache:user:device:id:"
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
	}

	for _, auth := range u.AuthMethods {
		if auth.AuthType == authmethod.Email {
			keys = append(keys, fmt.Sprintf("%s%s", cacheUserEmailPrefix, authmethod.CanonicalEmail(auth.AuthIdentifier)))
			break
		}
	}
	return keys
}

func (s *Subscribe) GetCacheKeys() []string {
	if s == nil {
		return []string{}
	}
	keys := make([]string, 0)

	if s.Token != "" {
		keys = append(keys, fmt.Sprintf("%s%s", cacheUserSubscribeTokenPrefix, s.Token))
	}
	if s.UserId != 0 {
		keys = append(keys, fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, s.UserId))
	}
	if s.Id != 0 {
		keys = append(keys, fmt.Sprintf("%s%d", cacheUserSubscribeIdPrefix, s.Id))
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
	}
	if a.AuthType == authmethod.Email && a.AuthIdentifier != "" {
		keys = append(keys, fmt.Sprintf("%s%s", cacheUserEmailPrefix, authmethod.CanonicalEmail(a.AuthIdentifier)))
	}
	return keys
}
