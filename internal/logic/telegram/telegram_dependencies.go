package telegram

import (
	"context"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// TelegramSessionStore reads short-lived account-binding sessions.
type TelegramSessionStore interface {
	Get(ctx context.Context, key string) (string, error)
}

// TelegramRedisStore supports both account-binding sessions and administrator
// command confirmations.
type TelegramRedisStore interface {
	TelegramSessionStore
	TelegramAdminActionStore
}

// TelegramAdminHandler handles administrator Telegram commands.
type TelegramAdminHandler interface {
	Handle(msg *tgbotapi.Message)
}

// TelegramLogicDependencies explicitly declares the collaborators used by
// general Telegram command dispatch and account binding.
type TelegramLogicDependencies struct {
	Messenger TelegramMessenger
	Sessions  TelegramSessionStore
	UserAuth  repository.UserAuthRepo
	UserCache repository.UserCacheRepo
	Admin     TelegramAdminHandler
}

// NewTelegramBotMessenger adapts a Telegram Bot API client to the command
// messenger port.
func NewTelegramBotMessenger(bot *tgbotapi.BotAPI) TelegramMessenger {
	return telegramBotMessenger{bot: bot}
}

// NewTelegramRedisStore adapts Redis to the binding-session and administrator
// confirmation ports.
func NewTelegramRedisStore(client *redis.Client) TelegramRedisStore {
	return redisTelegramStore{client: client}
}

type redisTelegramStore struct {
	client *redis.Client
}

func (s redisTelegramStore) Get(ctx context.Context, key string) (string, error) {
	return s.client.Get(ctx, key).Result()
}

func (s redisTelegramStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s redisTelegramStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}
