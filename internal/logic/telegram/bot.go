package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func GetTelegramConfig(ctx context.Context, svcCtx *svc.ServiceContext) (*dto.TelegramConfig, error) {

	data, err := svcCtx.Store.Auth().FindOneByMethod(ctx, "telegram")
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get Telegram config failed: %v", err.Error())
	}
	var telegramConfig auth.TelegramAuthConfig
	err = json.Unmarshal([]byte(data.Config), &telegramConfig)
	if err != nil {
		logger.WithContext(ctx).Error("unmarshal telegram config failed", logger.Field("error", err.Error()))
		return nil, err
	}

	return &dto.TelegramConfig{
		TelegramBotToken:      telegramConfig.BotToken,
		TelegramNotify:        *data.Enabled,
		TelegramWebHookDomain: telegramConfig.WebHookDomain,
	}, nil
}

func ApiLink(ctx context.Context, svcCtx *svc.ServiceContext, method string) string {
	cfg, err := GetTelegramConfig(ctx, svcCtx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[ApiLink] failed to get telegram config", logger.Field("error", err.Error()))
		return ""
	}
	return "https://api.telegram.org/bot" + cfg.TelegramBotToken + "/" + method
}

func SendUserMessage(ctx context.Context, svcCtx *svc.ServiceContext, u user.User, text string, parseMode string) {
	if !svcCtx.Config.Telegram.EnableNotify {
		return
	}
	apiURL := ApiLink(ctx, svcCtx, "sendMessage")
	if apiURL == "" {
		return
	}

	userTelegramChatId, ok := findTelegram(&u)
	if !ok {
		return
	}
	req, _ := http.NewRequest("GET", apiURL, nil)
	q := req.URL.Query()
	q.Add("chat_id", strconv.FormatInt(userTelegramChatId, 10))
	if parseMode == "markdown" {
		text = strings.ReplaceAll(text, "_", "\\_")
	}
	q.Add("text", text)
	q.Add("parse_mode", parseMode)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.WithContext(ctx).Errorw("[SendUserMessage] HTTP request failed",
			logger.Field("error", err.Error()),
			logger.Field("chat_id", userTelegramChatId),
		)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.WithContext(ctx).Errorw("[SendUserMessage] Telegram API returned non-OK status",
			logger.Field("status", resp.StatusCode),
			logger.Field("chat_id", userTelegramChatId),
			logger.Field("response", string(body)),
		)
	}
}

func SendAdminMessage(ctx context.Context, svcCtx *svc.ServiceContext, text string, parseMode string) {
	if !svcCtx.Config.Telegram.EnableNotify {
		return
	}
	apiURL := ApiLink(ctx, svcCtx, "sendMessage")
	if apiURL == "" {
		return
	}

	var adminTelegram []int64
	found := false
	adminTelegramJson, err := svcCtx.Redis.Get(ctx, config.AdminTelegramChatIdsKey).Result()
	if err == nil {
		err = json.Unmarshal([]byte(adminTelegramJson), &adminTelegram)
		if err == nil {
			found = true
		}
	}
	if !found {
		admins, err := svcCtx.Store.User().QueryAdminUsers(ctx)
		if err != nil {
			logger.WithContext(ctx).Error("[SendAdminMessage] query admin users failed", logger.Field("error", err.Error()))
			return
		}
		for _, admin := range admins {
			if telegram, ok := findTelegram(admin); ok {
				adminTelegram = append(adminTelegram, telegram)
			}
		}
		val, _ := json.Marshal(adminTelegram)
		_ = svcCtx.Redis.Set(ctx, config.AdminTelegramChatIdsKey, string(val), time.Duration(3600)*time.Second).Err()
	}
	if len(adminTelegram) == 0 {
		return
	}

	if parseMode == "markdown" {
		text = strings.ReplaceAll(text, "_", "\\_")
	}

	for _, telegram := range adminTelegram {
		req, _ := http.NewRequest("GET", apiURL, nil)
		q := req.URL.Query()
		q.Add("chat_id", strconv.FormatInt(telegram, 10))
		q.Add("text", text)
		q.Add("parse_mode", parseMode)
		req.URL.RawQuery = q.Encode()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.WithContext(ctx).Errorw("[SendAdminMessage] HTTP request failed",
				logger.Field("error", err.Error()),
				logger.Field("chat_id", telegram),
			)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			logger.WithContext(ctx).Errorw("[SendAdminMessage] Telegram API returned non-OK status",
				logger.Field("status", resp.StatusCode),
				logger.Field("chat_id", telegram),
				logger.Field("response", string(body)),
			)
		}
	}
}

func findTelegram(u *user.User) (int64, bool) {
	for _, item := range u.AuthMethods {
		if item.AuthType == "telegram" {
			// string to int64
			parseInt, err := strconv.ParseInt(item.AuthIdentifier, 10, 64)
			if err != nil {
				return 0, false
			}
			return parseInt, true
		}

	}
	return 0, false
}
