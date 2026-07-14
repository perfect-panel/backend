package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/internal/model/entity/announcement"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var cacheAnnouncementIdPrefix = "cache:announcement:id:"

// AnnouncementRepo announcement 数据访问接口
type AnnouncementRepo interface {
	Insert(ctx context.Context, data *announcement.Announcement) error
	FindOne(ctx context.Context, id int64) (*announcement.Announcement, error)
	Update(ctx context.Context, data *announcement.Announcement) error
	Delete(ctx context.Context, id int64) error
	GetAnnouncementListByPage(ctx context.Context, page, size int, filter announcement.Filter) (int64, []*announcement.Announcement, error)
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
}

var _ AnnouncementRepo = (*announcementRepo)(nil)

type announcementRepo struct {
	cache.CachedConn
	table string
}

func newAnnouncementRepo(db *gorm.DB, c *redis.Client) AnnouncementRepo {
	return &announcementRepo{
		CachedConn: cache.NewConn(db, c),
		table:      "announcement",
	}
}

func (m *announcementRepo) getCacheKeys(data *announcement.Announcement) []string {
	if data == nil {
		return []string{}
	}
	announcementIdKey := fmt.Sprintf("%s%v", cacheAnnouncementIdPrefix, data.Id)
	return []string{
		announcementIdKey,
	}
}

func (m *announcementRepo) Insert(ctx context.Context, data *announcement.Announcement) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
}

func (m *announcementRepo) FindOne(ctx context.Context, id int64) (*announcement.Announcement, error) {
	var resp announcement.Announcement
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&announcement.Announcement{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *announcementRepo) Update(ctx context.Context, data *announcement.Announcement) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Save(data).Error
	}, m.getCacheKeys(old)...)
}

func (m *announcementRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Delete(&announcement.Announcement{}, id).Error
	}, m.getCacheKeys(data)...)
}

// GetAnnouncementListByPage get announcement list by page
func (m *announcementRepo) GetAnnouncementListByPage(ctx context.Context, page, size int, filter announcement.Filter) (int64, []*announcement.Announcement, error) {
	var list []*announcement.Announcement
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&announcement.Announcement{})
		if filter.Show != nil {
			conn = conn.Where(clause.Eq{
				Column: clause.Column{Name: "show"},
				Value:  *filter.Show,
			})
		}
		if filter.Pinned != nil {
			conn = conn.Where("pinned = ?", *filter.Pinned)
		}
		if filter.Popup != nil {
			conn = conn.Where("popup = ?", *filter.Popup)
		}
		if filter.Search != "" {
			conn = conn.Scopes(orm.ContainsLike([]string{"title", "content"}, filter.Search))
		}
		return conn.Count(&total).Offset((page - 1) * size).Limit(size).Find(&list).Error
	})
	return total, list, err
}

func (m *announcementRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.TransactCtx(ctx, fn)
}
