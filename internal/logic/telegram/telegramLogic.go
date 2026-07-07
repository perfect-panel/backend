package telegram

import (
	"context"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type TelegramLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelegramLogic {
	return &TelegramLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TelegramLogic) TelegramLogic(req *tgbotapi.Update) {
	if req.Message != nil && req.Message.Text != "" {
		switch req.Message.Command() {
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
	} else {
		l.Logger.Error("[TelegramLogic] Message is empty")
	}
}

func (l *TelegramLogic) sendMessage(bot *tgbotapi.BotAPI, message string, userId int64) error {
	msg := tgbotapi.NewMessage(userId, message)
	msg.ParseMode = "Markdown"
	_, err := bot.Send(msg)
	return err
}

func (l *TelegramLogic) traffic(userId int64) error {
	return nil
}

func (l *TelegramLogic) bind(userId int64, token string) error {
	return nil
}

func (l *TelegramLogic) start(req *tgbotapi.Update) error {
	if req.Message.CommandArguments() == "" {
		return l.sendMessage(l.svcCtx.TelegramBot, "Please bind account!", req.Message.Chat.ID)
	} else {
		sessionId := req.Message.CommandArguments()
		// get session id from redis
		sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
		value, err := l.svcCtx.Redis.Get(context.Background(), sessionIdCacheKey).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			l.Errorw("TelegramLogic start Redis Get Error: ", logger.Field("error", err.Error()), logger.Field("session", sessionId))
			return l.sendMessage(l.svcCtx.TelegramBot, "Bind failed!", req.Message.Chat.ID)
		}
		if value == "" {
			l.Errorw("TelegramLogic start Redis Get Error: ", logger.Field("error", "session not found"), logger.Field("session", sessionId))
			return l.sendMessage(l.svcCtx.TelegramBot, "Bind failed!", req.Message.Chat.ID)
		}
		userId, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			l.Errorw("TelegramLogic start ParseInt Error: ", logger.Field("error", err.Error()), logger.Field("session", sessionId))
			return l.sendMessage(l.svcCtx.TelegramBot, "Bind failed!", req.Message.Chat.ID)
		}

		method, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(l.ctx, userId, "telegram")
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			l.Errorw("TelegramLogic start FindUserAuthMethodByPlatform Error: ", logger.Field("error", err.Error()), logger.Field("userId", userId))
			return l.sendMessage(l.svcCtx.TelegramBot, "Bind failed!", req.Message.Chat.ID)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, &user.AuthMethods{
				UserId:         userId,
				AuthType:       "telegram",
				AuthIdentifier: strconv.FormatInt(req.Message.Chat.ID, 10),
				Verified:       true,
				CreatedAt:      timeutil.Now(),
				UpdatedAt:      timeutil.Now(),
			}); err != nil {
				l.Errorw("TelegramLogic start InsertUserAuthMethod Error: ", logger.Field("error", err.Error()), logger.Field("userId", userId))
				return l.sendMessage(l.svcCtx.TelegramBot, "Bind failed!", req.Message.Chat.ID)
			}
		} else {
			method.AuthIdentifier = strconv.FormatInt(req.Message.Chat.ID, 10)
			if err := l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method); err != nil {
				l.Errorw("TelegramLogic start UpdateUserAuthMethod Error: ", logger.Field("error", err.Error()), logger.Field("userId", userId))
				return l.sendMessage(l.svcCtx.TelegramBot, "Bind failed!", req.Message.Chat.ID)
			}
		}
		// update user info to redis
		err = l.svcCtx.Store.User().UpdateUserCache(l.ctx, &user.User{
			Id: userId,
		})
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "update user cache failed")
		}

		text, err := tool.RenderTemplateToString(BindNotify, map[string]string{
			"Id":   strconv.FormatInt(userId, 10),
			"Time": timeutil.Now().Format("2006-01-02 15:04:05"),
		})
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "render template failed")
		}
		return l.sendMessage(l.svcCtx.TelegramBot, text, req.Message.Chat.ID)
	}
}
