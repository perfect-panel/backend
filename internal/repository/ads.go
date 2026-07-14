package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/internal/model/entity/ads"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var cacheAdsIdPrefix = "cache:ads:id:"

// AdsRepo ads 数据访问接口
type AdsRepo interface {
	Insert(ctx context.Context, data *ads.Ads) error
	FindOne(ctx context.Context, id int64) (*ads.Ads, error)
	Update(ctx context.Context, data *ads.Ads) error
	Delete(ctx context.Context, id int64) error
	GetAdsListByPage(ctx context.Context, page, size int, filter ads.Filter) (int64, []*ads.Ads, error)
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
}

var _ AdsRepo = (*adsRepo)(nil)

type adsRepo struct {
	cache.CachedConn
	table string
}

func newAdsRepo(db *gorm.DB, c *redis.Client) AdsRepo {
	return &adsRepo{
		CachedConn: cache.NewConn(db, c),
		table:      "ads",
	}
}

func (m *adsRepo) getCacheKeys(data *ads.Ads) []string {
	if data == nil {
		return []string{}
	}
	adsIdKey := fmt.Sprintf("%s%v", cacheAdsIdPrefix, data.Id)
	return []string{
		adsIdKey,
	}
}

func (m *adsRepo) Insert(ctx context.Context, data *ads.Ads) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
}

func (m *adsRepo) FindOne(ctx context.Context, id int64) (*ads.Ads, error) {
	var resp ads.Ads
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&ads.Ads{}).Where("id = ?", id).First(&resp).Error
	})
	return &resp, err
}

func (m *adsRepo) Update(ctx context.Context, data *ads.Ads) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Save(data).Error
	}, m.getCacheKeys(old)...)
}

func (m *adsRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Delete(&ads.Ads{}, id).Error
	}, m.getCacheKeys(data)...)
}

// GetAdsListByPage get ads list by page
func (m *adsRepo) GetAdsListByPage(ctx context.Context, page, size int, filter ads.Filter) (int64, []*ads.Ads, error) {
	var list []*ads.Ads
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&ads.Ads{})
		if filter.Status != nil {
			conn = conn.Where("status = ?", *filter.Status)
		}
		if filter.Search != "" {
			conn = conn.Scopes(orm.ContainsLike([]string{"title", "content"}, filter.Search))
		}
		return conn.Count(&total).Offset((page - 1) * size).Limit(size).Find(v).Error
	})
	return total, list, err
}

func (m *adsRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.TransactCtx(ctx, fn)
}
