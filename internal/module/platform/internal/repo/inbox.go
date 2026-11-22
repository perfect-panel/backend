package repo

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/repository/kernel"

	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"gorm.io/gorm"
)

var _ kernel.InboxRepo = (*inboxRepo)(nil)

type inboxRepo struct {
	db *gorm.DB
}

// NewInboxRepo builds the module-owned implementation.
func NewInboxRepo(db *gorm.DB) kernel.InboxRepo {
	return &inboxRepo{db: db}
}

func (m *inboxRepo) Find(ctx context.Context, consumer, eventKey string) (*inbox.Record, error) {
	var record inbox.Record
	err := m.db.WithContext(ctx).
		Where("consumer = ? AND event_key = ?", consumer, eventKey).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *inboxRepo) Insert(ctx context.Context, consumer, eventKey, result string) error {
	return m.db.WithContext(ctx).Create(&inbox.Record{
		Consumer: consumer,
		EventKey: eventKey,
		Result:   result,
	}).Error
}

func (m *inboxRepo) DeleteProcessedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := m.db.WithContext(ctx).
		Where("processed_at < ?", cutoff).
		Delete(&inbox.Record{})
	return result.RowsAffected, result.Error
}
