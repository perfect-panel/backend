package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	githuboauth "github.com/perfect-panel/server/pkg/oauth/github"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type BindOAuthLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Bind OAuth
func NewBindOAuthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindOAuthLogic {
	return &BindOAuthLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BindOAuthLogic) BindOAuth(req *dto.BindOAuthRequest) (resp *dto.BindOAuthResponse, err error) {
	if err := registerpolicy.EnsureMethodEnabled(l.ctx, l.svcCtx, req.Method); err != nil {
		return nil, err
	}
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
	default:
		l.Errorw("oauth login method not support: %v", logger.Field("method", req.Method))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "oauth login method not support: %v", req.Method)
	}
	if err != nil {
		l.Errorw("error bind oauth", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "error bind oauth: %v", err.Error())
	}
	return &dto.BindOAuthResponse{
		Redirect: uri,
	}, nil
}

func (l *BindOAuthLogic) google(req *dto.BindOAuthRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "google")
	if err != nil {
		return "", err
	}
	cfg := new(auth.GoogleAuthConfig)
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Errorw("error unmarshal google config", logger.Field("error", err.Error()))
		return "", err
	}
	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  req.Redirect,
	})
	// generate the state code
	code := random.KeyNew(32, 1)
	// save the state code
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("google:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		return "", err
	}
	uri := client.AuthCodeURL(code, oauth2.AccessTypeOffline)
	return uri, nil
}

func (l *BindOAuthLogic) facebook() (string, error) {
	return "", nil
}
func (l *BindOAuthLogic) apple(req *dto.BindOAuthRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "apple")
	if err != nil {
		return "", err
	}
	var cfg auth.AppleAuthConfig
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Errorw("error unmarshal apple config", logger.Field("error", err.Error()))
		return "", err
	}
	uri := "https://appleid.apple.com/auth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s&scope=name email&response_mode=form_post"
	// generate the state code
	code := random.KeyNew(32, 1)
	// save the state code
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("apple:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		l.Errorw("error save state code to redis", logger.Field("error", err.Error()))
	}
	return fmt.Sprintf(uri, cfg.ClientId, fmt.Sprintf("%s/v1/auth/oauth/callback/apple", cfg.RedirectURL), code), nil
}
func (l *BindOAuthLogic) github(req *dto.BindOAuthRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "github")
	if err != nil {
		return "", err
	}
	var cfg auth.GithubAuthConfig
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Errorw("error unmarshal github config", logger.Field("error", err.Error()))
		return "", err
	}
	client := githuboauth.New(&githuboauth.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  req.Redirect,
	})
	// generate the state code
	code := random.KeyNew(32, 1)
	// save the state code
	err = l.svcCtx.Redis.Set(l.ctx, fmt.Sprintf("github:%s", code), req.Redirect, 5*60*time.Second).Err()
	if err != nil {
		return "", err
	}
	uri := client.AuthCodeURL(code, oauth2.AccessTypeOffline)
	return uri, nil
}

func (l *BindOAuthLogic) telegram(req *dto.BindOAuthRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "telegram")
	if err != nil {
		return "", err
	}
	var cfg auth.TelegramAuthConfig
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Errorw("error unmarshal telegram config", logger.Field("error", err.Error()))
		return "", err
	}
	code := random.KeyNew(32, 1)
	return telegram.GenerateTelegramOAuthURL(cfg.BotToken, code, req.Redirect), nil
}
