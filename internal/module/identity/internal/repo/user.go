// Package repo holds the identity module's repository implementations: the
// user account rows, auth methods, devices and the cross-domain cache
// facade (ADR-001 step-6 preparation).
package repo

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/logger"
)

// Cache key prefixes shared with the user entity's key derivation.
const (
	cacheUserIdPrefix           = "cache:user:id:"
	cacheUserStatePrefix        = "cache:user:state:"
	cacheUserEmailPrefix        = "cache:user:email:v2:"
	cacheUserDeviceNumberPrefix = "cache:user:device:number:"
	cacheUserDeviceIdPrefix     = "cache:user:device:id:"
)

var _ repository.UserRepo = (*UserRepo)(nil)
var _ repository.UserAuthRepo = (*UserRepo)(nil)
var _ repository.UserDeviceRepo = (*UserRepo)(nil)
var _ repository.UserCacheRepo = (*UserRepo)(nil)

type UserRepo struct {
	cache.CachedConn
	table string
	// bridges are the identity bundle's cross-domain windows: the
	// subscription cache cascade, the subscription-membership filters and
	// billing's order statistics all go through them instead of touching
	// foreign tables from identity SQL.
	bridges repository.IdentityBridges
	// retrier redoes the cache invalidations of account writes that failed,
	// so a ban or deletion is not served from the cache for its lifetime
	// after a Redis hiccup; nil drops them, as the shared connection does.
	retrier *cache.InvalidationRetrier
}

// Option customizes a UserRepo.
type Option func(*UserRepo)

// WithInvalidationRetrier retries the failed cache invalidations of account
// writes through retrier.
func WithInvalidationRetrier(retrier *cache.InvalidationRetrier) Option {
	return func(m *UserRepo) { m.retrier = retrier }
}

// NewUserRepo builds the module-owned implementation over the shared cached
// connection; the bridges feed the cross-domain cascades and filters.
func NewUserRepo(conn cache.CachedConn, bridges repository.IdentityBridges, opts ...Option) *UserRepo {
	m := &UserRepo{
		CachedConn: conn,
		table:      "user",
		bridges:    bridges,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// execInvalidating runs exec and drops the cache keys it made stale. Unlike
// the shared connection's ExecCtx, a failed invalidation is not dropped: it
// is logged and handed to the retrier, so the cached account rows the
// request gate reads do not outlive a ban or deletion by the cache's
// lifetime. Inside a transaction the keys are queued as usual.
func (m *UserRepo) execInvalidating(ctx context.Context, exec cache.ExecCtxFn, keys ...string) error {
	if err := m.ExecNoCacheCtx(ctx, exec); err != nil {
		return err
	}
	m.invalidate(ctx, keys...)
	return nil
}

// invalidate drops the cache keys, retrying in the background when Redis
// refuses. The write is durable, so the failure is not the caller's.
func (m *UserRepo) invalidate(ctx context.Context, keys ...string) {
	if len(keys) == 0 {
		return
	}
	// The write may have committed although the request ended meanwhile, so
	// the cached rows are dropped without its cancellation.
	if err := m.CachedConn.DelCacheCtx(context.WithoutCancel(ctx), keys...); err != nil {
		logger.WithContext(ctx).Errorw("[UserRepo] cache invalidation failed; queued for retry",
			logger.Field("keys", len(keys)), logger.Field("error", err.Error()))
		m.retrier.Enqueue(keys...)
	}
}

// --- internal helpers ---

func (m *UserRepo) getCacheKeys(data *user.User) []string {
	if data == nil {
		return []string{}
	}
	return data.GetCacheKeys()
}

func (m *UserRepo) batchGetCacheKeys(users ...*user.User) []string {
	var keys []string
	for _, u := range users {
		keys = append(keys, u.GetCacheKeys()...)
	}
	return keys
}

// --- cache helpers ---

func (m *UserRepo) ClearUserCache(ctx context.Context, users ...*user.User) error {
	if len(users) == 0 {
		return nil
	}
	var keys []string
	for _, u := range users {
		if u != nil {
			keys = append(keys, u.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *UserRepo) ClearDeviceCache(ctx context.Context, devices ...*user.Device) error {
	if len(devices) == 0 {
		return nil
	}
	var keys []string
	for _, d := range devices {
		if d != nil {
			keys = append(keys, d.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *UserRepo) ClearAuthMethodCache(ctx context.Context, authMethods ...*user.AuthMethods) error {
	if len(authMethods) == 0 {
		return nil
	}
	var keys []string
	for _, a := range authMethods {
		if a != nil {
			keys = append(keys, a.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *UserRepo) BatchClearRelatedCache(ctx context.Context, u *user.User) error {
	if u == nil {
		return nil
	}
	return m.CachedConn.DelCacheCtx(ctx, m.relatedCacheKeys(ctx, u)...)
}

// relatedCacheKeys lists every cached projection of the account: its own
// rows, its bindings, its devices and its subscriptions.
func (m *UserRepo) relatedCacheKeys(ctx context.Context, u *user.User) []string {
	if u == nil {
		return nil
	}
	var allKeys []string
	allKeys = append(allKeys, u.GetCacheKeys()...)

	for _, auth := range u.AuthMethods {
		allKeys = append(allKeys, auth.GetCacheKeys()...)
	}

	for _, device := range u.UserDevices {
		allKeys = append(allKeys, device.GetCacheKeys()...)
	}

	subscribes, err := m.bridges.SubscriptionCache.QueryUserSubscribe(ctx, u.Id)
	if err != nil {
		logger.Errorf("failed to query user subscribes for cache clearing: %v", err)
	} else {
		for _, sub := range subscribes {
			subModel := &usersub.Subscribe{
				Id:          sub.Id,
				UserId:      sub.UserId,
				Token:       sub.Token,
				SubscribeId: sub.SubscribeId,
			}
			allKeys = append(allKeys, subModel.GetCacheKeys()...)
		}
	}
	return allKeys
}

// ClearSubscribeCache and UpdateUserSubscribeCache delegate to the
// subscription repo: the cache facade (UserCacheRepo) stays one object for
// its consumers while each domain owns its keys.
func (m *UserRepo) ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error {
	return m.bridges.SubscriptionCache.ClearSubscribeCache(ctx, data...)
}

func (m *UserRepo) UpdateUserSubscribeCache(ctx context.Context, data *usersub.Subscribe) error {
	return m.bridges.SubscriptionCache.UpdateUserSubscribeCache(ctx, data)
}
