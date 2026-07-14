package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	cacheAuthIdPrefix     = "cache:auth:id:"
	cacheAuthMethodPrefix = "cache:auth:method:"
)

// AuthRepo auth 数据访问接口
type AuthRepo interface {
	Insert(ctx context.Context, data *auth.Auth) error
	FindOne(ctx context.Context, id int64) (*auth.Auth, error)
	Update(ctx context.Context, data *auth.Auth) error
	Delete(ctx context.Context, id int64) error
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
	GetAuthListByPage(ctx context.Context) ([]*auth.Auth, error)
	FindOneByMethod(ctx context.Context, platform string) (*auth.Auth, error)
	FindAll(ctx context.Context) ([]*auth.Auth, error)
}

var _ AuthRepo = (*authRepo)(nil)

type authRepo struct {
	cache.CachedConn
	table string
}

func newAuthRepo(db *gorm.DB, c *redis.Client) AuthRepo {
	return &authRepo{
		CachedConn: cache.NewConn(db, c),
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
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&auth.Auth{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *authRepo) Update(ctx context.Context, data *auth.Auth) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		return db.Save(data).Error
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

func (m *authRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.TransactCtx(ctx, fn)
}

// GetAuthListByPage get auth list by page
func (m *authRepo) GetAuthListByPage(ctx context.Context) ([]*auth.Auth, error) {
	var list []*auth.Auth
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&auth.Auth{})
		return conn.Find(v).Error
	})
	return list, err
}

// FindOneByMethod find one by method
func (m *authRepo) FindOneByMethod(ctx context.Context, method string) (*auth.Auth, error) {
	var data auth.Auth
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&auth.Auth{}).Where("method = ?", method).First(v).Error
	})
	return &data, err
}

// FindAll find all
func (m *authRepo) FindAll(ctx context.Context) ([]*auth.Auth, error) {
	var list []*auth.Auth
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&auth.Auth{})
		return conn.Find(v).Error
	})
	return list, err
}
