package user

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/oauth/apple"
	githuboauth "github.com/perfect-panel/server/pkg/oauth/github"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const telegramBindAuthExpire = 86400

type BindOAuthCallbackLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Bind OAuth Callback
func NewBindOAuthCallbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindOAuthCallbackLogic {
	return &BindOAuthCallbackLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

type googleRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

func (l *BindOAuthCallbackLogic) BindOAuthCallback(req *dto.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	var err error
	switch req.Method {
	case "google":
		err = l.google(req)
	case "apple":
		err = l.apple(req)
	case "telegram":
		err = l.telegram(req)
	case "github":
		err = l.github(req)
	default:
		l.Errorw("oauth login method not support", logger.Field("method", req.Method))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "oauth login method not support: %v", req.Method)
	}
	if err != nil {
		l.Errorw("bind oauth callback failed: %v", logger.Field("error", err.Error()))
		return err
	}
	// update user info to redis
	err = l.svcCtx.Store.User().UpdateUserCache(l.ctx, u)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "update user cache failed")
	}

	return nil
}
func (l *BindOAuthCallbackLogic) google(req *dto.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	var request googleRequest
	err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request)
	if err != nil {
		l.Errorw("error CloneMapToStruct: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CloneMapToStruct failed")
	}
	// validate the state code
	redirect, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("google:%s", request.State)).Result()
	if err != nil {
		l.Errorw("error get google state code: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get google state code failed")
	}
	// get google config
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "google")
	if err != nil {
		l.Errorw("error find google auth method: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find google auth method failed")
	}
	var cfg auth.GoogleAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Errorw("error unmarshal google config: %v", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal google config failed")
	}
	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})
	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Errorw("error exchange google token: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange google token failed")
	}
	googleUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Errorw("error get google user info: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get google user info failed")
	}
	// query user info
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "google", googleUserInfo.OpenID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user auth method failed")
	}
	if userAuthMethod.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "google user already exists")
	}
	// bind google
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "google",
		AuthIdentifier: googleUserInfo.OpenID,
		Verified:       true,
	}
	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Errorw("error insert user auth method: %v", logger.Field("error", err.Error()))
		return err
	}
	return nil
}

func (l *BindOAuthCallbackLogic) apple(req *dto.BindOAuthCallbackRequest) error {
	// validate the state code
	_, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("apple:%s", req.Callback.(map[string]interface{})["state"])).Result()
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] Get State code error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple state code failed: %v", err.Error())
	}
	appleAuth, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "apple")
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] FindOneByMethod error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find apple auth method failed: %v", err.Error())
	}
	var appleCfg auth.AppleAuthConfig
	err = json.Unmarshal([]byte(appleAuth.Config), &appleCfg)
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] Unmarshal error", logger.Field("error", err.Error()), logger.Field("config", appleAuth.Config))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err.Error())
	}

	client, err := apple.New(apple.Config{
		ClientID:     appleCfg.ClientId,
		TeamID:       appleCfg.TeamID,
		KeyID:        appleCfg.KeyID,
		ClientSecret: appleCfg.ClientSecret,
		RedirectURI:  appleCfg.RedirectURL,
	})
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] New apple client error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "new apple client failed: %v", err.Error())
	}
	// verify web token
	resp, err := client.VerifyWebToken(l.ctx, req.Callback.(map[string]interface{})["code"].(string))
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] VerifyWebToken error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", err.Error())
	}
	if resp.Error != "" {
		l.Errorw("[BindOAuthCallbackLogic] VerifyWebToken error", logger.Field("error", resp.Error))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", resp.Error)
	}
	// query apple user unique id
	appleUnique, err := apple.GetUniqueID(resp.IDToken)
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] GetUniqueID error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple unique id failed: %v", err.Error())
	}
	// query user by apple unique id
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "apple", appleUnique)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("[BindOAuthCallbackLogic] FindUserAuthMethodByOpenID error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth method by openid failed: %v", err.Error())
	}
	if userAuthMethod.Id > 0 {
		l.Errorw("[BindOAuthCallbackLogic] User already exists")
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "apple user already exists")
	}
	// query user info
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// bind apple
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "apple",
		AuthIdentifier: appleUnique,
		Verified:       true,
	}
	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Errorw("[BindOAuthCallbackLogic] InsertUserAuthMethods error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert user auth method failed: %v", err.Error())
	}
	return nil
}

func (l *BindOAuthCallbackLogic) telegram(req *dto.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	callback, ok := req.Callback.(map[string]interface{})
	if !ok {
		l.Errorw("invalid telegram callback payload", logger.Field("callback_type", fmt.Sprintf("%T", req.Callback)))
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid telegram callback payload")
	}

	encodeText, ok := callback["tgAuthResult"].(string)
	if !ok || encodeText == "" {
		l.Errorw("telegram callback payload missing tgAuthResult")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid telegram callback payload")
	}

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "telegram")
	if err != nil {
		l.Errorw("find telegram auth method failed", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram auth method failed")
	}

	var cfg auth.TelegramAuthConfig
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Errorw("unmarshal telegram config failed", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal telegram config failed")
	}

	callbackData, err := telegram.ParseAndValidateBase64([]byte(encodeText), cfg.BotToken)
	if err != nil {
		l.Errorw("parse telegram callback failed", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse telegram callback failed")
	}

	if callbackData.Id == nil || callbackData.AuthDate == nil {
		l.Errorw("telegram callback payload missing required fields")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid telegram callback payload")
	}

	if timeutil.Now().Unix()-*callbackData.AuthDate > telegramBindAuthExpire {
		l.Errorw("telegram auth date expired",
			logger.Field("auth_date", *callbackData.AuthDate),
			logger.Field("current_time", timeutil.Now().Unix()),
			logger.Field("expire_seconds", telegramBindAuthExpire),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "auth date expired")
	}

	telegramUserID := fmt.Sprintf("%d", *callbackData.Id)

	existingByOpenID, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "telegram", telegramUserID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("find telegram user auth method by openid failed", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram user auth method failed")
	}
	if existingByOpenID.Id > 0 {
		if existingByOpenID.UserId == u.Id {
			return nil
		}
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "telegram user already exists")
	}

	existingByPlatform, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(l.ctx, u.Id, "telegram")
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("find telegram user auth method by platform failed", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram user auth method failed")
	}
	if existingByPlatform.Id > 0 {
		if existingByPlatform.AuthIdentifier == telegramUserID {
			return nil
		}
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "telegram already bound")
	}

	userAuthMethod := &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "telegram",
		AuthIdentifier: telegramUserID,
		Verified:       true,
	}

	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Errorw("insert telegram user auth method failed", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert telegram user auth method failed")
	}

	return nil
}

func (l *BindOAuthCallbackLogic) github(req *dto.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	var request googleRequest
	err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request)
	if err != nil {
		l.Errorw("error CloneMapToStruct: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CloneMapToStruct failed")
	}

	// validate the state code
	redirect, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("github:%s", request.State)).Result()
	if err != nil {
		l.Errorw("error get github state code: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get github state code failed")
	}

	// get github config
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "github")
	if err != nil {
		l.Errorw("error find github auth method: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find github auth method failed")
	}

	var cfg auth.GithubAuthConfig
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Errorw("error unmarshal github config: %v", logger.Field("config", authMethod.Config), logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal github config failed")
	}

	client := githuboauth.New(&githuboauth.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})

	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Errorw("error exchange github token: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange github token failed")
	}

	githubUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Errorw("error get github user info: %v", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get github user info failed")
	}

	// check if this GitHub account is already bound to another user
	openID := fmt.Sprintf("%d", githubUserInfo.OpenID)
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "github", openID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user auth method failed")
	}
	if userAuthMethod.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "github user already exists")
	}

	// bind github
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "github",
		AuthIdentifier: openID,
		Verified:       true,
	}
	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Errorw("error insert user auth method: %v", logger.Field("error", err.Error()))
		return err
	}
	return nil
}
