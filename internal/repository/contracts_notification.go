package repository

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
)

// TelegramTopicRepo persists the mapping between forum topics in the
// administrators' Telegram group and what each topic carries. Lookups return
// gorm.ErrRecordNotFound when no mapping exists.
type TelegramTopicRepo interface {
	Insert(ctx context.Context, data *telegramtopic.Topic) error
	FindByKindRef(ctx context.Context, chatID int64, kind uint8, refID int64) (*telegramtopic.Topic, error)
	FindByThread(ctx context.Context, chatID, threadID int64) (*telegramtopic.Topic, error)
	// UpdateThread repoints a mapping after its topic was deleted in
	// Telegram and had to be recreated.
	UpdateThread(ctx context.Context, id, threadID int64) error
	UpdateStatus(ctx context.Context, id int64, status uint8) error
}
