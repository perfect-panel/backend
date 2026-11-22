package repo

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"gorm.io/gorm"
)

var _ repository.ClientRepo = (*clientRepo)(nil)

type clientRepo struct {
	cache.CachedConn
}

const clientListCacheKey = "cache:subscribe:applications:v1"

// NewClientRepo builds the module-owned implementation.
func NewClientRepo(conn cache.CachedConn) repository.ClientRepo {
	return &clientRepo{CachedConn: conn}
}

func (m *clientRepo) Insert(ctx context.Context, data *client.SubscribeApplication) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&client.SubscribeApplication{}).Create(data).Error
	}, clientListCacheKey)
}

func (m *clientRepo) FindOne(ctx context.Context, id int64) (*client.SubscribeApplication, error) {
	var resp client.SubscribeApplication
	key := fmt.Sprintf("cache:subscribe:application:%d", id)
	if err := m.QueryCtx(ctx, &resp, key, func(conn *gorm.DB, v any) error {
		return conn.Model(&client.SubscribeApplication{}).Where("id = ?", id).First(v).Error
	}); err != nil {
		return nil, err
	}
	return &resp, nil
}

// applicationColumns are the client application settings an
// administrator's edit writes: every column but the id and the timestamps.
var applicationColumns = []string{
	"name", "icon", "description", "scheme", "user_agent", "is_default",
	"subscribe_template", "output_format", "default_params", "download_link",
}

// Update writes the application's settings (applicationColumns, plus the
// update timestamp) as data has them; the creation timestamp stays as stored.
func (m *clientRepo) Update(ctx context.Context, data *client.SubscribeApplication) error {
	if _, err := m.FindOne(ctx, data.Id); err != nil {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&client.SubscribeApplication{}).Where("id = ?", data.Id).Select(applicationColumns).Updates(data).Error
	}, clientListCacheKey, fmt.Sprintf("cache:subscribe:application:%d", data.Id))
}

func (m *clientRepo) Delete(ctx context.Context, id int64) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&client.SubscribeApplication{}).Where("id = ?", id).Delete(&client.SubscribeApplication{}).Error
	}, clientListCacheKey, fmt.Sprintf("cache:subscribe:application:%d", id))
}

func (m *clientRepo) List(ctx context.Context) ([]*client.SubscribeApplication, error) {
	var resp []*client.SubscribeApplication
	if err := m.QueryCtx(ctx, &resp, clientListCacheKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&client.SubscribeApplication{}).Order("id ASC").Find(v).Error
	}); err != nil {
		return nil, err
	}
	return resp, nil
}
