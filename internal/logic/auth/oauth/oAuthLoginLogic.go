package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	githuboauth "github.com/perfect-panel/server/pkg/oauth/github"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

type OAuthLoginLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// OAuth login
func NewOAuthLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthLoginLogic {
	return &OAuthLoginLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthLoginLogic) OAuthLogin(req *dto.OAthLoginRequest) (resp *dto.OAuthLoginResponse, err error) {
	var uri string
	switch req.Method {
	case "google":
		uri, err = l.google(req)
	case "apple":
		uri, err = l.apple(req)
	case "telegram":
		uri, err = l.telegram(req)
	case "github":
		uri, err = l.github(req)
	case "facebook":
		uri, err = l.facebook()

	}
	if err != nil {
		l.Errorw("OAuthLogin ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "OAuthLogin: %v", err.Error())
	}
	return &dto.OAuthLoginResponse{
		Redirect: uri,
	}, nil
}

func (l *OAuthLoginLogic) google(req *dto.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "google")
	if err != nil {
		return "", err
	}
	var cfg auth.GoogleAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Errorw("error unmarshal google config: %v", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return "", err
	}
	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  req.Redirect,
	})
	// generate the state code
	code := random.KeyNew(8, 1)
	// save the state code
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("google:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		return "", err
	}
	uri := client.AuthCodeURL(code, oauth2.AccessTypeOffline)
	return uri, nil
}

func (l *OAuthLoginLogic) facebook() (string, error) {
	return "", nil
}
func (l *OAuthLoginLogic) apple(req *dto.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "apple")
	if err != nil {
		return "", err
	}
	var cfg auth.AppleAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Errorw("error unmarshal apple config: %v", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return "", err
	}
	uri := "https://appleid.apple.com/auth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s&scope=name email&response_mode=form_post"
	// generate the state code
	code := random.KeyNew(8, 1)
	// save the state code under correct apple prefix
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("apple:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		l.Errorw("error save state code to redis: %v", logger.Field("code", code), logger.Field("error", err.Error()))
	}
	return fmt.Sprintf(uri, cfg.ClientId, fmt.Sprintf("%s/v1/auth/oauth/callback/apple", cfg.RedirectURL), code), nil
}
func (l *OAuthLoginLogic) github(req *dto.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "github")
	if err != nil {
		return "", err
	}
	var cfg auth.GithubAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Errorw("error unmarshal github config: %v", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return "", err
	}
	client := githuboauth.New(&githuboauth.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  req.Redirect,
	})
	// generate the state code
	code := random.KeyNew(8, 1)
	// save the state code
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("github:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		return "", err
	}
	uri := client.AuthCodeURL(code, oauth2.AccessTypeOffline)
	return uri, nil
}
func (l *OAuthLoginLogic) telegram(req *dto.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "telegram")
	if err != nil {
		return "", err
	}
	var cfg auth.TelegramAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Errorw("error unmarshal telegram config: %v", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return "", err
	}
	// generate the state code
	code := random.KeyNew(8, 1)
	// save the state code under correct telegram prefix
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("telegram:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		l.Errorw("error save state code to redis", logger.Field("code", code), logger.Field("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "error save state code to redis")
	}
	return telegram.GenerateTelegramOAuthURL(cfg.BotToken, code, req.Redirect), nil
}
