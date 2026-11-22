package telegram

import (
	"context"
	"fmt"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// TelegramSessionStore consumes the short-lived account-binding tokens and
// holds the binding locks. Take consumes a token in one step, so two chats
// redeeming the same deep link cannot both receive it; the time the token
// had left comes with it, so a redemption that fails for a reason the user
// can fix puts the token back without extending the link's life.
type TelegramSessionStore interface {
	// Take returns and removes the key's value with the time it had left; a
	// missing key reads as redis.Nil.
	Take(ctx context.Context, key string) (value string, ttl time.Duration, err error)
	// Set stores value under key for ttl.
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// Acquire takes the lock key for ttl and reports whether it was free.
	Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, key string) error
}

// TelegramRedisStore supports account-binding sessions, administrator
// command confirmations, and the support relay rate limit.
type TelegramRedisStore interface {
	TelegramSessionStore
	TelegramAdminActionStore
	TelegramRelayLimiter
}

// TelegramAdminHandler handles administrator Telegram commands.
type TelegramAdminHandler interface {
	Handle(ctx context.Context, msg *models.Message)
}

// TelegramRelayLimiter caps how fast one chat may relay support messages
// into the admin group.
type TelegramRelayLimiter interface {
	// Allow reports whether the chat may relay another message right now.
	// shouldNotify is true only for the first rejection of a window, so a
	// flood is answered with one notice instead of one per message.
	Allow(ctx context.Context, chatID int64) (allowed, shouldNotify bool)
}

// BotDependencies explicitly declares the collaborators used by update
// routing: user commands and account binding in the private chat, and the
// group-side relays. The group fields may be left zero, which turns every
// group feature off.
type BotDependencies struct {
	Messenger     TelegramMessenger
	Sessions      TelegramSessionStore
	Accounts      Accounts
	Subscriptions Subscriptions
	Admin         TelegramAdminHandler

	// GroupChatID returns the validated admin group; zero disables group
	// routing. Read per call because re-initialisation may change it.
	GroupChatID func() int64
	Topics      repository.TelegramTopicRepo
	TopicClient TelegramTopicClient
	Tickets     Tickets
	Limiter     TelegramRelayLimiter
}

// NewTelegramBotMessenger adapts a Telegram Bot API client to the command
// messenger port.
func NewTelegramBotMessenger(bot *tgbot.Bot) TelegramMessenger {
	return telegramBotMessenger{bot: bot}
}

// NewTelegramBotCommandRegistrar adapts a Telegram Bot API client to the
// command-menu port.
func NewTelegramBotCommandRegistrar(bot *tgbot.Bot) TelegramCommandRegistrar {
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

// takeScript is GETDEL that also reports the key's remaining life: the
// read, the PTTL and the delete run as one script, which keeps them atomic
// on Redis versions older than 6.2, the oldest the install guide supports. A
// missing key returns the nil reply, read as redis.Nil.
var takeScript = redis.NewScript(`
local value = redis.call("GET", KEYS[1])
if not value then
  return false
end
local ttl = redis.call("PTTL", KEYS[1])
redis.call("DEL", KEYS[1])
return {value, ttl}
`)

// take runs takeScript and decodes its reply.
func (s redisTelegramStore) take(ctx context.Context, key string) (string, time.Duration, error) {
	reply, err := takeScript.Run(ctx, s.client, []string{key}).Slice()
	if err != nil {
		return "", 0, err
	}
	if len(reply) != 2 {
		return "", 0, fmt.Errorf("take %s: unexpected reply %v", key, reply)
	}
	value, ok := reply[0].(string)
	if !ok {
		return "", 0, fmt.Errorf("take %s: unexpected value %v", key, reply[0])
	}
	// PTTL answers -1 for a key without expiry and -2 for a key that vanished
	// between the read and the query; neither is a life to restore.
	ttl := time.Duration(0)
	if millis, ok := reply[1].(int64); ok && millis > 0 {
		ttl = time.Duration(millis) * time.Millisecond
	}
	return value, ttl, nil
}

func (s redisTelegramStore) Take(ctx context.Context, key string) (string, time.Duration, error) {
	return s.take(ctx, key)
}

func (s redisTelegramStore) GetDel(ctx context.Context, key string) (string, error) {
	value, _, err := s.take(ctx, key)
	return value, err
}

func (s redisTelegramStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s redisTelegramStore) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, 1, ttl).Result()
}

func (s redisTelegramStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

// supportRelayPerMinute caps one chat's relayed messages. Telegram itself
// throttles bots at roughly twenty messages per minute per group, so one
// flooding user must not exhaust the whole group's budget.
const supportRelayPerMinute = 15

// Allow implements a fixed one-minute window. The TTL is created (SETNX)
// before the first increment so a crash between the two commands can never
// leave an expiry-less counter that locks the chat out forever. Errors fail
// open: the limit protects against floods, not against Redis being down.
func (s redisTelegramStore) Allow(ctx context.Context, chatID int64) (allowed, shouldNotify bool) {
	key := fmt.Sprintf("tg:support:rl:%d", chatID)
	if err := s.client.SetNX(ctx, key, 0, time.Minute).Err(); err != nil {
		return true, false
	}
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return true, false
	}
	return count <= supportRelayPerMinute, count == supportRelayPerMinute+1
}
