package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type TelegramLogic struct {
	logger.Logger
	ctx  context.Context
	deps TelegramLogicDependencies
}

func NewTelegramLogic(ctx context.Context, deps TelegramLogicDependencies) *TelegramLogic {
	return &TelegramLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *TelegramLogic) TelegramLogic(req *tgbotapi.Update) {
	if req.Message == nil || req.Message.Text == "" {
		l.Logger.Error("[TelegramLogic] Message is empty")
		return
	}
	cmd := req.Message.Command()
	if isAdminCommand(cmd) {
		l.deps.Admin.Handle(req.Message)
		return
	}
	switch cmd {
	case "traffic":
		if err := l.traffic(req.Message.Chat.ID); err != nil {
			l.Logger.Error("[TelegramLogic] Traffic Error: ", logger.Field("error", err.Error()), logger.Field("command", req.Message.Command()), logger.Field("chat_id", req.Message.Chat.ID))
		}
	case "bind":
		if err := l.bind(req.Message.Chat.ID, req.Message.CommandArguments()); err != nil {
			l.Logger.Error("[TelegramLogic] Bind Error: ", logger.Field("error", err.Error()), logger.Field("command", req.Message.Command()), logger.Field("chat_id", req.Message.Chat.ID))
		}
	case "start":
		if err := l.start(req); err != nil {
			l.Logger.Error("[TelegramLogic] Start Error: ", logger.Field("error", err.Error()), logger.Field("command", req.Message.Command()), logger.Field("chat_id", req.Message.Chat.ID), logger.Field("text", req.Message.Text))
		}
	}
}

func isAdminCommand(cmd string) bool {
	switch cmd {
	case "dash", "tickets", "tickets_waiting", "tk", "rp", "close", "reopen",
		"user", "user_sub", "user_log", "reset", "toggle", "ban", "help", "h":
		return true
	}
	if strings.HasPrefix(cmd, "confirm_") || strings.HasPrefix(cmd, "cancel_") {
		return true
	}
	return false
}

func (l *TelegramLogic) sendMessage(message string, userID int64) error {
	return l.deps.Messenger.Send(userID, message)
}

type telegramBotMessenger struct {
	bot *tgbotapi.BotAPI
}

func (m telegramBotMessenger) Send(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "Markdown"
	_, err := m.bot.Send(msg)
	return err
}

func (l *TelegramLogic) traffic(userId int64) error {
	return nil
}

func (l *TelegramLogic) bind(userId int64, token string) error {
	if token == "" {
		return l.sendMessage("Please provide a bind token. Usage: /bind <token>", userId)
	}

	// Look up the session from Redis using the token as session ID
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, token)
	value, err := l.deps.Sessions.Get(context.Background(), sessionIdCacheKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			l.Errorw("TelegramLogic bind token not found or expired", logger.Field("token", token))
			return l.sendMessage("Bind token is invalid or expired. Please request a new one.", userId)
		}
		l.Errorw("TelegramLogic bind Redis Get Error", logger.Field("error", err.Error()), logger.Field("token", token))
		return l.sendMessage("Bind failed. Please try again later.", userId)
	}

	bindUserId, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		l.Errorw("TelegramLogic bind ParseInt Error", logger.Field("error", err.Error()), logger.Field("value", value))
		return l.sendMessage("Bind failed. Invalid session data.", userId)
	}

	chatIdStr := strconv.FormatInt(userId, 10)

	// Check if this Chat ID is already bound to another user
	existingByChatId, err := l.deps.UserAuth.FindUserAuthMethodByOpenID(l.ctx, "telegram", chatIdStr)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("TelegramLogic bind FindUserAuthMethodByOpenID Error", logger.Field("error", err.Error()), logger.Field("chatId", userId))
		return l.sendMessage("Bind failed. Please try again later.", userId)
	}
	if existingByChatId.Id > 0 && existingByChatId.UserId != bindUserId {
		l.Infow("Telegram account already bound to another user",
			logger.Field("chatId", userId),
			logger.Field("existingUserId", existingByChatId.UserId),
		)
		return l.sendMessage("This Telegram account is already bound to another user.", userId)
	}

	// Check if the target user already has Telegram bound
	existingByUser, err := l.deps.UserAuth.FindUserAuthMethodByPlatform(l.ctx, bindUserId, "telegram")
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("TelegramLogic bind FindUserAuthMethodByPlatform Error", logger.Field("error", err.Error()), logger.Field("bindUserId", bindUserId))
		return l.sendMessage("Bind failed. Please try again later.", userId)
	}
	if err == nil && existingByUser.Id > 0 {
		// Same chat ID, already bound — nothing to do
		if existingByUser.AuthIdentifier == chatIdStr {
			return l.sendMessage("This account is already bound to your Telegram.", userId)
		}
		l.Infow("User already bound to a different Telegram account",
			logger.Field("bindUserId", bindUserId),
			logger.Field("existingChatId", existingByUser.AuthIdentifier),
			logger.Field("newChatId", userId),
		)
		return l.sendMessage("Your account is already bound to a different Telegram account. Please unbind it first.", userId)
	}

	// Create the binding
	if err := l.deps.UserAuth.InsertUserAuthMethods(l.ctx, &user.AuthMethods{
		UserId:         bindUserId,
		AuthType:       "telegram",
		AuthIdentifier: chatIdStr,
		Verified:       true,
		CreatedAt:      timeutil.Now(),
		UpdatedAt:      timeutil.Now(),
	}); err != nil {
		l.Errorw("TelegramLogic bind InsertUserAuthMethod Error", logger.Field("error", err.Error()), logger.Field("bindUserId", bindUserId))
		return l.sendMessage("Bind failed. Please try again later.", userId)
	}

	// Update user cache
	err = l.deps.UserCache.UpdateUserCache(l.ctx, &user.User{
		Id: bindUserId,
	})
	if err != nil {
		l.Errorw("TelegramLogic bind UpdateUserCache Error", logger.Field("error", err.Error()), logger.Field("bindUserId", bindUserId))
	}

	text, err := tool.RenderTemplateToString(BindNotify, map[string]string{
		"Id":   strconv.FormatInt(bindUserId, 10),
		"Time": timeutil.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		l.Errorw("TelegramLogic bind RenderTemplate Error", logger.Field("error", err.Error()))
		return l.sendMessage("Bound successfully!", userId)
	}
	return l.sendMessage(text, userId)
}

func (l *TelegramLogic) start(req *tgbotapi.Update) error {
	if req.Message.CommandArguments() == "" {
		return l.sendMessage("Please bind account!", req.Message.Chat.ID)
	}

	sessionId := req.Message.CommandArguments()
	chatIdStr := strconv.FormatInt(req.Message.Chat.ID, 10)

	// Get session from Redis
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	value, err := l.deps.Sessions.Get(context.Background(), sessionIdCacheKey)
	if err != nil && !errors.Is(err, redis.Nil) {
		l.Errorw("TelegramLogic start Redis Get Error", logger.Field("error", err.Error()), logger.Field("session", sessionId))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}
	if value == "" {
		l.Errorw("TelegramLogic start session not found or expired", logger.Field("session", sessionId))
		return l.sendMessage("Session expired. Please request a new bind link.", req.Message.Chat.ID)
	}

	userId, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		l.Errorw("TelegramLogic start ParseInt Error", logger.Field("error", err.Error()), logger.Field("session", sessionId))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}

	// Check if this Chat ID is already bound to another user
	existingByChatId, err := l.deps.UserAuth.FindUserAuthMethodByOpenID(l.ctx, "telegram", chatIdStr)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			l.Errorw("TelegramLogic start FindUserAuthMethodByOpenID Error", logger.Field("error", err.Error()), logger.Field("chatId", req.Message.Chat.ID))
			return l.sendMessage("Bind failed!", req.Message.Chat.ID)
		}
	}
	if existingByChatId.Id > 0 && existingByChatId.UserId != userId {
		l.Infow("Telegram account already bound to another user, cannot rebind",
			logger.Field("chatId", req.Message.Chat.ID),
			logger.Field("existingUserId", existingByChatId.UserId),
			logger.Field("newUserId", userId),
		)
		return l.sendMessage("This Telegram account is already bound to another user.", req.Message.Chat.ID)
	}

	// Check if the target user already has a Telegram binding
	method, err := l.deps.UserAuth.FindUserAuthMethodByPlatform(l.ctx, userId, "telegram")
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("TelegramLogic start FindUserAuthMethodByPlatform Error", logger.Field("error", err.Error()), logger.Field("userId", userId))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}

	if err == nil && method.Id > 0 {
		// Already bound to the same chat ID — nothing to do
		if method.AuthIdentifier == chatIdStr {
			return l.sendMessage("Your account is already bound to this Telegram account.", req.Message.Chat.ID)
		}
		// Already bound to a different chat ID — DON'T overwrite silently
		l.Infow("User already bound to a different Telegram account, cannot rebind via start",
			logger.Field("userId", userId),
			logger.Field("existingChatId", method.AuthIdentifier),
			logger.Field("newChatId", req.Message.Chat.ID),
		)
		return l.sendMessage("Your account is already bound to a different Telegram account. Please unbind it first.", req.Message.Chat.ID)
	}

	// No existing binding — create a new one
	if err := l.deps.UserAuth.InsertUserAuthMethods(l.ctx, &user.AuthMethods{
		UserId:         userId,
		AuthType:       "telegram",
		AuthIdentifier: chatIdStr,
		Verified:       true,
		CreatedAt:      timeutil.Now(),
		UpdatedAt:      timeutil.Now(),
	}); err != nil {
		l.Errorw("TelegramLogic start InsertUserAuthMethod Error", logger.Field("error", err.Error()), logger.Field("userId", userId))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}

	// Update user cache
	err = l.deps.UserCache.UpdateUserCache(l.ctx, &user.User{
		Id: userId,
	})
	if err != nil {
		l.Errorw("TelegramLogic start UpdateUserCache Error", logger.Field("error", err.Error()), logger.Field("userId", userId))
	}

	text, err := tool.RenderTemplateToString(BindNotify, map[string]string{
		"Id":   strconv.FormatInt(userId, 10),
		"Time": timeutil.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		l.Errorw("TelegramLogic start RenderTemplate Error", logger.Field("error", err.Error()))
		return l.sendMessage("Bound successfully!", req.Message.Chat.ID)
	}
	return l.sendMessage(text, req.Message.Chat.ID)
}
