package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/oauth/apple"
	githuboauth "github.com/perfect-panel/server/pkg/oauth/github"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const (
	OAuthGoogle    = "google"
	OAuthApple     = "apple"
	OAuthTelegram  = "telegram"
	OAuthGithub    = "github"
	AuthEmail      = "email"
	AuthExpire     = 86400
	TelegramDomain = "ppanel.com"
)

type oauthRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}
type OAuthLoginGetTokenLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewOAuthLoginGetTokenLogic OAuth login get token
func NewOAuthLoginGetTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthLoginGetTokenLogic {
	return &OAuthLoginGetTokenLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthLoginGetTokenLogic) OAuthLoginGetToken(req *types.OAuthLoginGetTokenRequest, ip, userAgent string) (resp *types.LoginResponse, err error) {
	requestID := uuidx.NewUUID().String()
	loginStatus := false
	var userInfo *user.User

	l.Infow("oauth login request started",
		logger.Field("request_id", requestID),
		logger.Field("method", req.Method),
		logger.Field("ip", ip),
		logger.Field("user_agent", userAgent),
	)

	defer func() {
		l.recordLoginStatus(loginStatus, userInfo, ip, userAgent, requestID, req.Method)
	}()

	userInfo, err = l.handleOAuthProvider(req, requestID, ip, userAgent)
	if err != nil {
		return nil, err
	}

	token, err := l.generateToken(userInfo, requestID)
	if err != nil {
		return nil, err
	}

	loginStatus = true
	return &types.LoginResponse{Token: token}, nil
}

func (l *OAuthLoginGetTokenLogic) google(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := timeutil.Now()
	l.Infow("google oauth processing started",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGoogle),
	)

	var request oauthRequest
	if err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request); err != nil {
		l.Errorw("failed to parse google callback data",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGoogle),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse callback data failed: %v", err)
	}

	l.Debugw("google oauth state validation started",
		logger.Field("request_id", requestID),
		logger.Field("state", request.State),
	)

	redirect, err := l.validateStateCode(OAuthGoogle, request.State, requestID)
	if err != nil {
		return nil, err
	}

	cfg, err := l.getGoogleConfig(requestID)
	if err != nil {
		return nil, err
	}

	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})

	l.Debugw("exchanging google authorization code for token",
		logger.Field("request_id", requestID),
		logger.Field("redirect_url", redirect),
	)

	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Errorw("failed to exchange google authorization code",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGoogle),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange token failed: %v", err)
	}

	l.Debugw("fetching google user information",
		logger.Field("request_id", requestID),
	)

	googleUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Errorw("failed to get google user info",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGoogle),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get user info failed: %v", err)
	}

	l.Infow("google oauth processing completed",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGoogle),
		logger.Field("openid", googleUserInfo.OpenID),
		logger.Field("email", googleUserInfo.Email),
		logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthGoogle, googleUserInfo.OpenID, googleUserInfo.Email, googleUserInfo.Picture, requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) apple(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := timeutil.Now()
	l.Infow("apple oauth processing started",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthApple),
	)

	callback := req.Callback.(map[string]interface{})
	state, _ := callback["state"].(string)
	code, _ := callback["code"].(string)

	l.Debugw("apple oauth state validation started",
		logger.Field("request_id", requestID),
		logger.Field("state", state),
	)

	if _, err := l.validateStateCode(OAuthApple, state, requestID); err != nil {
		return nil, err
	}

	cfg, err := l.getAppleConfig(requestID)
	if err != nil {
		return nil, err
	}

	client, err := apple.New(apple.Config{
		ClientID:     cfg.ClientId,
		TeamID:       cfg.TeamID,
		KeyID:        cfg.KeyID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURL,
	})
	if err != nil {
		l.Errorw("failed to create apple client",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "new apple client failed: %v", err)
	}

	l.Debugw("verifying apple web token",
		logger.Field("request_id", requestID),
	)

	resp, err := client.VerifyWebToken(l.ctx, code)
	if err != nil {
		l.Errorw("failed to verify apple web token",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", err)
	}

	if resp.Error != "" {
		l.Errorw("apple web token verification returned error",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("apple_error", resp.Error),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", resp.Error)
	}

	appleUnique, err := apple.GetUniqueID(resp.IDToken)
	if err != nil {
		l.Errorw("failed to get apple unique id",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple unique id failed: %v", err)
	}

	appleUserInfo, err := apple.GetClaims(resp.AccessToken)
	if err != nil {
		l.Errorw("failed to get apple user claims",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple user info failed: %v", err)
	}

	email := ""
	if emailVal, ok := (*appleUserInfo)["email"]; ok {
		email, _ = emailVal.(string)
	}

	l.Infow("apple oauth processing completed",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthApple),
		logger.Field("unique_id", appleUnique),
		logger.Field("email", email),
		logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthApple, appleUnique, email, "", requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) telegram(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := timeutil.Now()
	l.Infow("telegram oauth processing started",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthTelegram),
	)

	cfg, err := l.getTelegramConfig(requestID)
	if err != nil {
		return nil, err
	}

	encodeText, _ := req.Callback.(map[string]interface{})["tgAuthResult"].(string)
	l.Debugw("parsing telegram callback data",
		logger.Field("request_id", requestID),
		logger.Field("data_length", len(encodeText)),
	)

	callbackData, err := telegram.ParseAndValidateBase64([]byte(encodeText), cfg.BotToken)
	if err != nil {
		l.Errorw("failed to parse telegram callback data",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthTelegram),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse telegram callback failed: %v", err)
	}

	l.Debugw("validating telegram auth date",
		logger.Field("request_id", requestID),
		logger.Field("auth_date", *callbackData.AuthDate),
		logger.Field("current_time", timeutil.Now().Unix()),
	)

	if timeutil.Now().Unix()-*callbackData.AuthDate > AuthExpire {
		l.Errorw("telegram auth date expired",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthTelegram),
			logger.Field("auth_date", *callbackData.AuthDate),
			logger.Field("current_time", timeutil.Now().Unix()),
			logger.Field("expire_seconds", AuthExpire),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "auth date expired")
	}

	userID := fmt.Sprintf("%v", *callbackData.Id)
	email := fmt.Sprintf("%v@%s", *callbackData.Id, TelegramDomain)
	avatar := ""
	if callbackData.PhotoUrl != nil {
		avatar = *callbackData.PhotoUrl
	}

	l.Infow("telegram oauth processing completed",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthTelegram),
		logger.Field("user_id", userID),
		logger.Field("email", email),
		logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthTelegram, userID, email, avatar, requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) github(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := timeutil.Now()
	l.Infow("github oauth processing started",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGithub),
	)

	var request oauthRequest
	if err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request); err != nil {
		l.Errorw("failed to parse github callback data",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGithub),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse callback data failed: %v", err)
	}

	l.Debugw("github oauth state validation started",
		logger.Field("request_id", requestID),
		logger.Field("state", request.State),
	)

	redirect, err := l.validateStateCode(OAuthGithub, request.State, requestID)
	if err != nil {
		return nil, err
	}

	cfg, err := l.getGithubConfig(requestID)
	if err != nil {
		return nil, err
	}

	client := githuboauth.New(&githuboauth.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})

	l.Debugw("exchanging github authorization code for token",
		logger.Field("request_id", requestID),
		logger.Field("redirect_url", redirect),
	)

	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Errorw("failed to exchange github authorization code",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGithub),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange token failed: %v", err)
	}

	l.Debugw("fetching github user information",
		logger.Field("request_id", requestID),
	)

	githubUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Errorw("failed to get github user info",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGithub),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get user info failed: %v", err)
	}

	l.Infow("github oauth processing completed",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGithub),
		logger.Field("openid", githubUserInfo.OpenID),
		logger.Field("email", githubUserInfo.Email),
		logger.Field("login", githubUserInfo.Login),
		logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthGithub, fmt.Sprintf("%d", githubUserInfo.OpenID), githubUserInfo.Email, githubUserInfo.Avatar, requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) register(email, avatar, method, openid, requestID, ip, userAgent string) (*user.User, error) {
	startTime := timeutil.Now()
	l.Infow("user registration started",
		logger.Field("request_id", requestID),
		logger.Field("auth_method", method),
		logger.Field("email", email),
		logger.Field("openid", openid),
	)

	if l.svcCtx.Config.Invite.ForcedInvite {
		l.Errorw("registration blocked due to forced invite policy",
			logger.Field("request_id", requestID),
			logger.Field("auth_method", method),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
	}

	var userInfo *user.User
	var trialSubscribe *user.Subscribe
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		if email != "" {
			l.Debugw("checking if email already exists",
				logger.Field("request_id", requestID),
				logger.Field("email", email),
			)
			if err := l.checkEmailExists(store, email, requestID); err != nil {
				return err
			}
		}

		l.Debugw("creating new user record",
			logger.Field("request_id", requestID),
			logger.Field("avatar", avatar),
		)

		userInfo = &user.User{Avatar: avatar, OnlyFirstPurchase: &l.svcCtx.Config.Invite.OnlyFirstPurchase}
		if err := store.User().Insert(l.ctx, userInfo); err != nil {
			l.Errorw("failed to create user record",
				logger.Field("request_id", requestID),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create user info failed: %v", err)
		}

		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		l.Debugw("updating user refer code",
			logger.Field("request_id", requestID),
			logger.Field("user_id", userInfo.Id),
			logger.Field("refer_code", userInfo.ReferCode),
		)

		if err := store.User().Update(l.ctx, userInfo); err != nil {
			l.Errorw("failed to update refer code",
				logger.Field("request_id", requestID),
				logger.Field("user_id", userInfo.Id),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update refer code failed: %v", err)
		}

		if err := l.createAuthMethod(store, userInfo.Id, method, openid, requestID); err != nil {
			return err
		}

		if email != "" {
			if err := l.createAuthMethod(store, userInfo.Id, AuthEmail, email, requestID); err != nil {
				return err
			}
		}

		if l.svcCtx.Config.Register.EnableTrial {
			l.Debugw("activating trial subscription",
				logger.Field("request_id", requestID),
				logger.Field("user_id", userInfo.Id),
			)
			var trialErr error
			trialSubscribe, trialErr = l.activeTrial(store, userInfo.Id, requestID)
			if trialErr != nil {
				return trialErr
			}
		}

		return nil
	})

	if err != nil {
		l.Errorw("user registration failed",
			logger.Field("request_id", requestID),
			logger.Field("auth_method", method),
			logger.Field("error", err.Error()),
			logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
		)
		return userInfo, err
	}
	clearTrialSubscribeCache(l.ctx, l.svcCtx, trialSubscribe)

	l.Infow("user registration completed successfully",
		logger.Field("request_id", requestID),
		logger.Field("user_id", userInfo.Id),
		logger.Field("auth_method", method),
		logger.Field("email", email),
		logger.Field("refer_code", userInfo.ReferCode),
		logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
	)

	// Register log
	registerLog := log.Register{
		AuthMethod: method,
		Identifier: openid,
		RegisterIP: ip,
		UserAgent:  userAgent,
		Timestamp:  timeutil.Now().UnixMilli(),
	}
	content, _ := registerLog.Marshal()

	err = l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
		Type:     log.TypeRegister.Uint8(),
		Date:     timeutil.Now().Format("2006-01-02"),
		ObjectID: userInfo.Id,
		Content:  string(content),
	})
	if err != nil {
		l.Errorw("failed to insert register log",
			logger.Field("request_id", requestID),
			logger.Field("user_id", userInfo.Id),
			logger.Field("ip", ip),
			logger.Field("error", err.Error()),
		)
	}

	return userInfo, err
}

func (l *OAuthLoginGetTokenLogic) checkEmailExists(store repository.Store, email, requestID string) error {
	userInfo, err := store.User().FindOneByEmail(l.ctx, email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("failed to check email existence",
			logger.Field("request_id", requestID),
			logger.Field("email", email),
			logger.Field("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check email exists failed: %v", err)
	}
	if err == nil && userInfo != nil && userInfo.Id != 0 {
		l.Errorw("email already exists for another user",
			logger.Field("request_id", requestID),
			logger.Field("email", email),
			logger.Field("existing_user_id", userInfo.Id),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "user email exist: %v", email)
	}
	l.Debugw("email availability confirmed",
		logger.Field("request_id", requestID),
		logger.Field("email", email),
	)
	return nil
}

func (l *OAuthLoginGetTokenLogic) createAuthMethod(store repository.Store, userID int64, authType, identifier, requestID string) error {
	l.Debugw("creating auth method",
		logger.Field("request_id", requestID),
		logger.Field("user_id", userID),
		logger.Field("auth_type", authType),
		logger.Field("identifier", identifier),
	)

	authMethod := &user.AuthMethods{
		UserId:         userID,
		AuthType:       authType,
		AuthIdentifier: identifier,
		Verified:       true,
	}
	if err := store.User().InsertUserAuthMethods(l.ctx, authMethod); err != nil {
		l.Errorw("failed to create auth method",
			logger.Field("request_id", requestID),
			logger.Field("user_id", userID),
			logger.Field("auth_type", authType),
			logger.Field("identifier", identifier),
			logger.Field("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create auth method failed: %v", err)
	}

	l.Debugw("auth method created successfully",
		logger.Field("request_id", requestID),
		logger.Field("user_id", userID),
		logger.Field("auth_type", authType),
		logger.Field("auth_method_id", authMethod.Id),
	)
	return nil
}

func (l *OAuthLoginGetTokenLogic) recordLoginStatus(loginStatus bool, userInfo *user.User, ip, userAgent, requestID, authType string) {

	if userInfo != nil && userInfo.Id != 0 {
		loginLog := log.Login{
			Method:    authType,
			LoginIP:   ip,
			UserAgent: userAgent,
			Success:   loginStatus,
			Timestamp: timeutil.Now().UnixMilli(),
		}
		content, _ := loginLog.Marshal()
		if err := l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
			Type:     log.TypeLogin.Uint8(),
			Date:     timeutil.Now().Format("2006-01-02"),
			ObjectID: userInfo.Id,
			Content:  string(content),
		}); err != nil {
			l.Errorw("failed to insert login log",
				logger.Field("request_id", requestID),
				logger.Field("user_id", userInfo.Id),
				logger.Field("ip", ip),
				logger.Field("error", err.Error()),
			)
		}
	}
}

func (l *OAuthLoginGetTokenLogic) handleOAuthProvider(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	l.Debugw("handling oauth provider",
		logger.Field("request_id", requestID),
		logger.Field("provider", req.Method),
	)

	switch req.Method {
	case OAuthGoogle:
		return l.google(req, requestID, ip, userAgent)
	case OAuthApple:
		return l.apple(req, requestID, ip, userAgent)
	case OAuthTelegram:
		return l.telegram(req, requestID, ip, userAgent)
	case OAuthGithub:
		return l.github(req, requestID, ip, userAgent)
	default:
		l.Errorw("unsupported oauth login method",
			logger.Field("request_id", requestID),
			logger.Field("method", req.Method),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "oauth login method not supported: %v", req.Method)
	}
}

func (l *OAuthLoginGetTokenLogic) generateToken(userInfo *user.User, requestID string) (string, error) {
	startTime := timeutil.Now()
	sessionId := uuidx.NewUUID().String()

	l.Debugw("generating jwt token",
		logger.Field("request_id", requestID),
		logger.Field("user_id", userInfo.Id),
		logger.Field("session_id", sessionId),
	)

	token, err := jwt.NewJwtToken(
		l.svcCtx.Config.JwtAuth.AccessSecret,
		timeutil.Now().Unix(),
		l.svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userInfo.Id),
		jwt.WithOption("SessionId", sessionId),
	)
	if err != nil {
		l.Errorw("failed to generate jwt token",
			logger.Field("request_id", requestID),
			logger.Field("user_id", userInfo.Id),
			logger.Field("error", err.Error()),
		)
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "token generate error: %v", err)
	}

	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err = l.svcCtx.Redis.Set(l.ctx, sessionIdCacheKey, userInfo.Id, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		l.Errorw("failed to cache session id",
			logger.Field("request_id", requestID),
			logger.Field("user_id", userInfo.Id),
			logger.Field("session_id", sessionId),
			logger.Field("error", err.Error()),
		)
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "set session id error: %v", err)
	}

	l.Infow("jwt token generated successfully",
		logger.Field("request_id", requestID),
		logger.Field("user_id", userInfo.Id),
		logger.Field("session_id", sessionId),
		logger.Field("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return token, nil
}

func (l *OAuthLoginGetTokenLogic) validateStateCode(provider, state, requestID string) (string, error) {
	stateKey := fmt.Sprintf("%s:%s", provider, state)
	l.Debugw("validating oauth state code",
		logger.Field("request_id", requestID),
		logger.Field("provider", provider),
		logger.Field("state_key", stateKey),
	)

	redirect, err := l.svcCtx.Redis.Get(l.ctx, stateKey).Result()
	if err != nil {
		l.Errorw("failed to validate state code",
			logger.Field("request_id", requestID),
			logger.Field("provider", provider),
			logger.Field("state_key", stateKey),
			logger.Field("error", err.Error()),
		)
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get %s state code failed: %v", provider, err)
	}

	l.Debugw("state code validated successfully",
		logger.Field("request_id", requestID),
		logger.Field("provider", provider),
		logger.Field("redirect_url", redirect),
	)
	return redirect, nil
}

func (l *OAuthLoginGetTokenLogic) getGoogleConfig(requestID string) (*auth.GoogleAuthConfig, error) {
	l.Debugw("fetching google oauth config",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGoogle),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthGoogle)
	if err != nil {
		l.Errorw("failed to find google auth method",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGoogle),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find google auth method failed: %v", err)
	}

	var cfg auth.GoogleAuthConfig
	if err = cfg.Unmarshal(authMethod.Config); err != nil {
		l.Errorw("failed to unmarshal google config",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGoogle),
			logger.Field("config", authMethod.Config),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal google config failed: %v", err)
	}

	l.Debugw("google oauth config loaded successfully",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGoogle),
		logger.Field("client_id", cfg.ClientId),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) getAppleConfig(requestID string) (*auth.AppleAuthConfig, error) {
	l.Debugw("fetching apple oauth config",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthApple),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthApple)
	if err != nil {
		l.Errorw("failed to find apple auth method",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find apple auth method failed: %v", err)
	}

	var cfg auth.AppleAuthConfig
	if err = cfg.Unmarshal(authMethod.Config); err != nil {
		l.Errorw("failed to unmarshal apple config",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthApple),
			logger.Field("config", authMethod.Config),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err)
	}

	l.Debugw("apple oauth config loaded successfully",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthApple),
		logger.Field("client_id", cfg.ClientId),
		logger.Field("team_id", cfg.TeamID),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) getTelegramConfig(requestID string) (*auth.TelegramAuthConfig, error) {
	l.Debugw("fetching telegram oauth config",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthTelegram),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthTelegram)
	if err != nil {
		l.Errorw("failed to find telegram auth method",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthTelegram),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram auth method failed: %v", err)
	}

	var cfg auth.TelegramAuthConfig
	if err = json.Unmarshal([]byte(authMethod.Config), &cfg); err != nil {
		l.Errorw("failed to unmarshal telegram config",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthTelegram),
			logger.Field("config", authMethod.Config),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal telegram config failed: %v", err)
	}

	l.Debugw("telegram oauth config loaded successfully",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthTelegram),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) getGithubConfig(requestID string) (*auth.GithubAuthConfig, error) {
	l.Debugw("fetching github oauth config",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGithub),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthGithub)
	if err != nil {
		l.Errorw("failed to find github auth method",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGithub),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find github auth method failed: %v", err)
	}

	var cfg auth.GithubAuthConfig
	if err = cfg.Unmarshal(authMethod.Config); err != nil {
		l.Errorw("failed to unmarshal github config",
			logger.Field("request_id", requestID),
			logger.Field("provider", OAuthGithub),
			logger.Field("config", authMethod.Config),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal github config failed: %v", err)
	}

	l.Debugw("github oauth config loaded successfully",
		logger.Field("request_id", requestID),
		logger.Field("provider", OAuthGithub),
		logger.Field("client_id", cfg.ClientId),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) findOrRegisterUser(authType, openID, email, avatar, requestID, ip, userAgent string) (*user.User, error) {
	l.Debugw("finding or registering user",
		logger.Field("request_id", requestID),
		logger.Field("auth_type", authType),
		logger.Field("openid", openID),
		logger.Field("email", email),
	)

	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, authType, openID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			l.Infow("user not found, starting registration",
				logger.Field("request_id", requestID),
				logger.Field("auth_type", authType),
				logger.Field("openid", openID),
				logger.Field("email", email),
			)
			return l.register(email, avatar, authType, openID, requestID, ip, userAgent)
		}
		l.Errorw("failed to find user auth method by openid",
			logger.Field("request_id", requestID),
			logger.Field("auth_type", authType),
			logger.Field("openid", openID),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth method by openid failed: %v", err)
	}

	l.Debugw("found existing user auth method",
		logger.Field("request_id", requestID),
		logger.Field("auth_type", authType),
		logger.Field("user_id", userAuthMethod.UserId),
	)

	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, userAuthMethod.UserId)
	if err != nil {
		l.Errorw("failed to find user by id",
			logger.Field("request_id", requestID),
			logger.Field("user_id", userAuthMethod.UserId),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user info failed: %v", err)
	}

	l.Infow("existing user found successfully",
		logger.Field("request_id", requestID),
		logger.Field("user_id", userInfo.Id),
		logger.Field("auth_type", authType),
	)

	return userInfo, nil
}

func (l *OAuthLoginGetTokenLogic) activeTrial(store repository.Store, uid int64, requestID string) (*user.Subscribe, error) {
	l.Debugw("fetching trial subscription template",
		logger.Field("request_id", requestID),
		logger.Field("user_id", uid),
		logger.Field("trial_subscribe_id", l.svcCtx.Config.Register.TrialSubscribe),
	)

	sub, err := store.Subscribe().FindOne(l.ctx, l.svcCtx.Config.Register.TrialSubscribe)
	if err != nil {
		l.Errorw("failed to find trial subscription template",
			logger.Field("request_id", requestID),
			logger.Field("user_id", uid),
			logger.Field("trial_subscribe_id", l.svcCtx.Config.Register.TrialSubscribe),
			logger.Field("error", err.Error()),
		)
		return nil, err
	}

	startTime := timeutil.Now()
	expireTime := tool.AddTime(l.svcCtx.Config.Register.TrialTimeUnit, l.svcCtx.Config.Register.TrialTime, startTime)
	subscribeToken := uuidx.SubscribeToken(fmt.Sprintf("Trial-%v-%s", uid, uuidx.NewUUID().String()))
	subscribeUUID := uuidx.NewUUID().String()

	l.Debugw("creating trial subscription",
		logger.Field("request_id", requestID),
		logger.Field("user_id", uid),
		logger.Field("subscribe_id", sub.Id),
		logger.Field("start_time", startTime),
		logger.Field("expire_time", expireTime),
		logger.Field("traffic", sub.Traffic),
		logger.Field("token", subscribeToken),
		logger.Field("uuid", subscribeUUID),
	)

	userSub := &user.Subscribe{
		Id:          0,
		UserId:      uid,
		OrderId:     0,
		SubscribeId: sub.Id,
		StartTime:   startTime,
		ExpireTime:  expireTime,
		Traffic:     sub.Traffic,
		Download:    0,
		Upload:      0,
		Token:       subscribeToken,
		UUID:        subscribeUUID,
		Status:      1,
	}

	if err := store.User().InsertSubscribe(l.ctx, userSub); err != nil {
		l.Errorw("failed to insert trial subscription",
			logger.Field("request_id", requestID),
			logger.Field("user_id", uid),
			logger.Field("error", err.Error()),
		)
		return nil, err
	}

	l.Infow("trial subscription activated successfully",
		logger.Field("request_id", requestID),
		logger.Field("user_id", uid),
		logger.Field("subscribe_id", sub.Id),
		logger.Field("expire_time", expireTime),
		logger.Field("traffic", sub.Traffic),
	)
	return userSub, nil
}
