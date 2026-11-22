package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/pkg/cache"
	"gorm.io/gorm"
)

var (
	cacheAuthIdPrefix     = "cache:auth:id:"
	cacheAuthMethodPrefix = "cache:auth:method:"
)

var _ repository.AuthRepo = (*authRepo)(nil)

type authRepo struct {
	cache.CachedConn
	table string
}

// NewAuthRepo builds the module-owned implementation over the shared
// cached connection.
func NewAuthRepo(conn cache.CachedConn) repository.AuthRepo {
	return &authRepo{
		CachedConn: conn,
		table:      "auth_config",
	}
}

//nolint:unused
func (m *authRepo) batchGetCacheKeys(Auths ...*auth.Auth) []string {
	var keys []string
	for _, a := range Auths {
		keys = append(keys, m.getCacheKeys(a)...)
	}
	return keys
}

func (m *authRepo) getCacheKeys(data *auth.Auth) []string {
	if data == nil {
		return []string{}
	}
	authIdKey := fmt.Sprintf("%s%v", cacheAuthIdPrefix, data.Id)
	platformKey := fmt.Sprintf("%s%s", cacheAuthMethodPrefix, data.Method)
	cacheKeys := []string{
		authIdKey,
		platformKey,
	}
	return cacheKeys
}

func (m *authRepo) Insert(ctx context.Context, data *auth.Auth) error {
	err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *authRepo) FindOne(ctx context.Context, id int64) (*auth.Auth, error) {
	var resp auth.Auth
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&auth.Auth{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Update writes the method's configuration and, when the snapshot carries
// one, its enabled flag. Only those columns are written: a whole-row save of
// a stale snapshot would revert the method name and creation time to what
// the caller loaded.
func (m *authRepo) Update(ctx context.Context, data *auth.Auth) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	columns := map[string]any{"config": data.Config}
	if data.Enabled != nil {
		columns["enabled"] = *data.Enabled
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&auth.Auth{}).Where("id = ?", data.Id).Updates(columns).Error
	}, m.getCacheKeys(old)...)
	return err
}

func (m *authRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		return db.Delete(&auth.Auth{}, id).Error
	}, m.getCacheKeys(data)...)
	return err
}

// GetAuthListByPage get auth list by page
func (m *authRepo) GetAuthListByPage(ctx context.Context) ([]*auth.Auth, error) {
	var list []*auth.Auth
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		conn = conn.Model(&auth.Auth{})
		return conn.Find(v).Error
	})
	return list, err
}

// FindOneByMethod find one by method
func (m *authRepo) FindOneByMethod(ctx context.Context, method string) (*auth.Auth, error) {
	var data auth.Auth
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&auth.Auth{}).Where("method = ?", method).First(v).Error
	})
	return &data, err
}

// FindAll find all
func (m *authRepo) FindAll(ctx context.Context) ([]*auth.Auth, error) {
	var list []*auth.Auth
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		conn = conn.Model(&auth.Auth{})
		return conn.Find(v).Error
	})
	return list, err
}
