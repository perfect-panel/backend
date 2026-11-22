// Package repo holds the support module's repository implementations:
// tickets, announcements, ads and documents, each cached in Redis. The module
// facade exports them through NewRepoBuilder.
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/orm"
	"gorm.io/gorm"
)

var (
	cacheTicketIdPrefix     = "cache:ticket:id:"
	cacheTicketDetailPrefix = "cache:ticket:detail:"
)

var _ repository.TicketRepo = (*ticketRepo)(nil)

type ticketRepo struct {
	cache.CachedConn
	table string
}

// NewTicketRepo builds the module-owned implementation over the shared
// cached connection.
func NewTicketRepo(conn cache.CachedConn) repository.TicketRepo {
	return &ticketRepo{
		CachedConn: conn,
		table:      "ticket",
	}
}

//nolint:unused
func (m *ticketRepo) batchGetCacheKeys(Tickets ...*ticket.Ticket) []string {
	var keys []string
	for _, ticket := range Tickets {
		keys = append(keys, m.getCacheKeys(ticket)...)
	}
	return keys

}

func (m *ticketRepo) getCacheKeys(data *ticket.Ticket) []string {
	if data == nil {
		return []string{}
	}
	ticketIdKey := fmt.Sprintf("%s%v", cacheTicketIdPrefix, data.Id)
	cacheKeys := []string{
		ticketIdKey,
	}
	return cacheKeys
}

func (m *ticketRepo) Insert(ctx context.Context, data *ticket.Ticket) error {
	err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *ticketRepo) FindOne(ctx context.Context, id int64) (*ticket.Ticket, error) {
	var resp ticket.Ticket
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {

		return conn.Model(&ticket.Ticket{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Update rewrites the ticket's mutable columns from data; the creation time
// stays, and a missing row is not inserted, which a whole-row save would do.
func (m *ticketRepo) Update(ctx context.Context, data *ticket.Ticket) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&ticket.Ticket{}).Where("id = ?", data.Id).
			Select("title", "description", "user_id", "status").
			Updates(data).Error
	}, m.getCacheKeys(old)...)
	return err
}

func (m *ticketRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		return db.Delete(&ticket.Ticket{}, id).Error
	}, m.getCacheKeys(data)...)
	return err
}

// QueryTicketDetail returns the ticket details.
func (m *ticketRepo) QueryTicketDetail(ctx context.Context, id int64) (*ticket.Details, error) {
	var data *ticket.Details
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&ticket.Ticket{}).Where("id = ?", id).Preload("Follows", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC, id ASC")
		}).First(v).Error
	})
	return data, err
}

// InsertTicketFollow inserts a follow record.
func (m *ticketRepo) InsertTicketFollow(ctx context.Context, data *ticket.Follow) error {
	key := fmt.Sprintf("%s%v", cacheTicketDetailPrefix, data.TicketId)
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&ticket.Follow{}).Create(data).Error
	}, key)
}

// QueryTicketList returns the ticket list.
func (m *ticketRepo) QueryTicketList(ctx context.Context, page, size int, userId int64, status *uint8, search string) (int64, []*ticket.Ticket, error) {
	var data []*ticket.Ticket
	var total int64
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		query := conn.Model(&ticket.Ticket{})
		if userId > 0 {
			query = query.Where("user_id = ?", userId)
		}
		if status != nil {
			query = query.Where("status = ?", status)
		} else {
			query = query.Where("status != ?", 4)
		}
		if search != "" {
			query = query.Scopes(orm.ContainsLike([]string{"title", "description"}, search))
		}
		return query.Count(&total).Order("id desc").Limit(size).Offset((page - 1) * size).Find(v).Error
	})
	return total, data, err
}

// UpdateTicketStatus updates the ticket status.
func (m *ticketRepo) UpdateTicketStatus(ctx context.Context, id, userId int64, status uint8) error {
	key := fmt.Sprintf("%s%v", cacheTicketDetailPrefix, id)
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		conn = conn.Model(&ticket.Ticket{})
		if userId > 0 {
			conn = conn.Where("user_id = ?", userId)
		}
		return conn.Where("id = ?", id).Update("status", status).Error
	}, key)
}

// QueryWaitReplyTotal returns the total number of tickets that are waiting for a reply.
func (m *ticketRepo) QueryWaitReplyTotal(ctx context.Context) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&ticket.Ticket{}).Where("status = ?", ticket.Pending).Count(&total).Error
	})
	return total, err
}
