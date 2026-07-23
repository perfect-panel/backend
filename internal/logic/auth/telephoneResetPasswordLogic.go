package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type TelephoneResetPasswordLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Reset password
func NewTelephoneResetPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelephoneResetPasswordLogic {
	return &TelephoneResetPasswordLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TelephoneResetPasswordLogic) TelephoneResetPassword(req *dto.TelephoneResetPasswordRequest) (resp *dto.LoginResponse, err error) {
	if err := registerpolicy.EnsureMethodEnabled(l.ctx, l.svcCtx, registerpolicy.MethodMobile); err != nil {
		return nil, err
	}
	code := req.Code

	phoneNumber, err := phone.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
	}

	// if the email verification is enabled, the verification code is required
	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.Security, phoneNumber)
	if err := common.ValidateVerificationCode(l.ctx, l.svcCtx.Redis, cacheKey, code, false); err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}

	authMethods, err := l.svcCtx.Store.UserAuth().FindUserAuthMethodByOpenID(l.ctx, "mobile", phoneNumber)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("FindOneByTelephone Error", logger.Field("error", err))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	}
	if authMethods.UserId == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user telephone exist: %v", phoneNumber)
	}

	// Check if the user exists
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, authMethods.UserId)
	if err != nil {
		l.Errorw("FindOneByTelephone Error", logger.Field("error", err))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	}
	if err := common.ValidateVerificationCode(l.ctx, l.svcCtx.Redis, cacheKey, code, true); err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}

	// Generate password
	pwd := tool.EncodePassWord(req.Password)
	userInfo.Password = pwd
	userInfo.Algo = tool.PasswordAlgoArgon2id
	userInfo.Salt = ""
	err = l.svcCtx.Store.User().Update(l.ctx, userInfo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "update user password failed: %v", err.Error())
	}

	// Bind device to user if identifier is provided
	if req.Identifier != "" {
		bindLogic := NewBindDeviceLogic(l.ctx, l.svcCtx)
		if err := bindLogic.BindDeviceToUser(req.Identifier, req.IP, req.UserAgent, userInfo.Id); err != nil {
			l.Errorw("failed to bind device to user",
				logger.Field("user_id", userInfo.Id),
				logger.Field("identifier", req.Identifier),
				logger.Field("error", err.Error()),
			)
			// Don't fail register if device binding fails, just log the error
		}
	}
	if l.ctx.Value(constant.LoginType) != nil {
		req.LoginType = l.ctx.Value(constant.LoginType).(string)
	}
	// Generate session id
	sessionId := uuidx.NewUUID().String()
	// Generate token
	token, err := jwt.NewJwtToken(
		l.svcCtx.Config.JwtAuth.AccessSecret,
		timeutil.Now().Unix(),
		l.svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userInfo.Id),
		jwt.WithOption("SessionId", sessionId),
		jwt.WithOption("LoginType", req.LoginType),
	)
	if err != nil {
		l.Errorw("[UserLogin] token generate error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "token generate error: %v", err.Error())
	}
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err = l.svcCtx.Redis.Set(l.ctx, sessionIdCacheKey, userInfo.Id, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "set session id error: %v", err.Error())
	}
	defer func() {
		if token != "" && userInfo.Id != 0 {
			loginLog := log.Login{
				Method:    "mobile",
				LoginIP:   req.IP,
				UserAgent: req.UserAgent,
				Success:   token != "",
				Timestamp: timeutil.Now().UnixMilli(),
			}
			content, _ := loginLog.Marshal()
			if err := l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
				Id:       0,
				Type:     log.TypeLogin.Uint8(),
				Date:     timeutil.Now().Format("2006-01-02"),
				ObjectID: userInfo.Id,
				Content:  string(content),
			}); err != nil {
				l.Errorw("failed to insert login log",
					logger.Field("user_id", userInfo.Id),
					logger.Field("ip", req.IP),
					logger.Field("error", err.Error()),
				)
			}
		}
	}()
	return &dto.LoginResponse{
		Token: token,
	}, nil
}
