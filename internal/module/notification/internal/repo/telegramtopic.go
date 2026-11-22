// Package repo holds the notification module's repository implementation: the
// mapping of forum topics in the administrators' Telegram group. The module
// facade exports it through NewRepoBuilder.
package repo

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/repository"
	"gorm.io/gorm"
)

var _ repository.TelegramTopicRepo = (*telegramTopicRepo)(nil)

type telegramTopicRepo struct {
	db *gorm.DB
}

// NewTelegramTopicRepo builds the module-owned implementation.
func NewTelegramTopicRepo(db *gorm.DB) repository.TelegramTopicRepo {
	return &telegramTopicRepo{db: db}
}

func (m *telegramTopicRepo) Insert(ctx context.Context, data *telegramtopic.Topic) error {
	return m.db.WithContext(ctx).Create(data).Error
}

func (m *telegramTopicRepo) FindByKindRef(ctx context.Context, chatID int64, kind uint8, refID int64) (*telegramtopic.Topic, error) {
	var topic telegramtopic.Topic
	err := m.db.WithContext(ctx).
		Where("chat_id = ? AND kind = ? AND ref_id = ?", chatID, kind, refID).
		First(&topic).Error
	if err != nil {
		return nil, err
	}
	return &topic, nil
}

func (m *telegramTopicRepo) FindByThread(ctx context.Context, chatID, threadID int64) (*telegramtopic.Topic, error) {
	var topic telegramtopic.Topic
	err := m.db.WithContext(ctx).
		Where("chat_id = ? AND thread_id = ?", chatID, threadID).
		First(&topic).Error
	if err != nil {
		return nil, err
	}
	return &topic, nil
}

func (m *telegramTopicRepo) UpdateThread(ctx context.Context, id, threadID int64) error {
	return m.db.WithContext(ctx).Model(&telegramtopic.Topic{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"thread_id": threadID,
			"status":    telegramtopic.StatusActive,
		}).Error
}

func (m *telegramTopicRepo) UpdateStatus(ctx context.Context, id int64, status uint8) error {
	return m.db.WithContext(ctx).Model(&telegramtopic.Topic{}).
		Where("id = ?", id).
		Update("status", status).Error
}
