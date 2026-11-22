package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/orm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var cacheAnnouncementIdPrefix = "cache:announcement:id:"

var _ repository.AnnouncementRepo = (*announcementRepo)(nil)

type announcementRepo struct {
	cache.CachedConn
	table string
}

// NewAnnouncementRepo builds the module-owned implementation over the shared
// cached connection.
func NewAnnouncementRepo(conn cache.CachedConn) repository.AnnouncementRepo {
	return &announcementRepo{
		CachedConn: conn,
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
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&announcement.Announcement{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Update rewrites the announcement's mutable columns from data; the
// creation time stays, and a missing row is not inserted, which a whole-row
// save would do.
func (m *announcementRepo) Update(ctx context.Context, data *announcement.Announcement) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&announcement.Announcement{}).Where("id = ?", data.Id).
			Select("title", "content", "show", "pinned", "popup").
			Updates(data).Error
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
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
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
