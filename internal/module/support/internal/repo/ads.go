package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
)

var cacheAdsIdPrefix = "cache:ads:id:"

var _ repository.AdsRepo = (*adsRepo)(nil)

type adsRepo struct {
	cache.CachedConn
	table string
}

// NewAdsRepo builds the module-owned implementation over the shared
// cached connection.
func NewAdsRepo(conn cache.CachedConn) repository.AdsRepo {
	return &adsRepo{
		CachedConn: conn,
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
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&ads.Ads{}).Where("id = ?", id).First(&resp).Error
	})
	return &resp, err
}

// Update rewrites the ad's mutable columns from data; the creation time
// stays, and a missing row is not inserted, which a whole-row save would do.
func (m *adsRepo) Update(ctx context.Context, data *ads.Ads) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&ads.Ads{}).Where("id = ?", data.Id).
			Select("title", "type", "content", "description", "target_url", "start_time", "end_time", "status").
			Updates(data).Error
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
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		conn = conn.Model(&ads.Ads{})
		if filter.Status != nil {
			conn = conn.Where("status = ?", *filter.Status)
		}
		if filter.Search != "" {
			conn = conn.Scopes(orm.ContainsLike([]string{"title", "content"}, filter.Search))
		}
		if filter.ActiveAt != nil {
			conn = conn.Scopes(adsActiveAt(*filter.ActiveAt))
		}
		return conn.Count(&total).Offset((page - 1) * size).Limit(size).Find(v).Error
	})
	return total, list, err
}

// adsActiveAt keeps the ads whose schedule covers at: start <= at < end. An
// unset bound — NULL, or the Unix epoch the admin API stores for an omitted
// time — leaves that side open. An unset start already sorts before any
// instant; an unset end has to be matched explicitly, and with a margin: the
// timestamp columns keep the wall clock of the process zone a row was written
// under, so the epoch reads 1970-01-01 08:00 in a row written under the
// image's Asia/Shanghai and 00:00 in one written under UTC, and the epoch
// rendered in the current zone would miss the rows written under another.
// Every end before 1971 is unset: no schedule ends that early.
func adsActiveAt(at time.Time) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("(start_time IS NULL OR start_time <= ?)", at).
			Where("(end_time IS NULL OR end_time < ? OR end_time > ?)", unsetEndLimit(), at)
	}
}

// unsetEndLimit is the instant every wall clock of the unset-bound epoch
// falls before. It is built per query: the application zone is only loaded
// at startup.
func unsetEndLimit() time.Time {
	return time.Date(1971, time.January, 1, 0, 0, 0, 0, timeutil.Location())
}
