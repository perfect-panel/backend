package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type DeviceLoginLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Device Login
func NewDeviceLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeviceLoginLogic {
	return &DeviceLoginLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeviceLoginLogic) DeviceLogin(req *dto.DeviceLoginRequest) (resp *dto.LoginResponse, err error) {
	if !l.svcCtx.Config.Device.Enable {
		return nil, xerr.NewErrMsg("Device login is disabled")
	}

	loginStatus := false
	var userInfo *user.User
	// Record login status
	defer func() {
		if userInfo != nil && userInfo.Id != 0 {
			loginLog := log.Login{
				Method:    "device",
				LoginIP:   req.IP,
				UserAgent: req.UserAgent,
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
					logger.Field("user_id", userInfo.Id),
					logger.Field("ip", req.IP),
					logger.Field("error", err.Error()),
				)
			}
		}
	}()

	// Check if device exists by identifier
	deviceInfo, err := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, req.Identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Device not found, create new user and device
			userInfo, err = l.registerUserAndDevice(req)
			if err != nil {
				return nil, err
			}
		} else {
			l.Errorw("query device failed",
				logger.Field("identifier", req.Identifier),
				logger.Field("error", err.Error()),
			)
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query device failed: %v", err.Error())
		}
	} else {
		// Device found, get user info
		userInfo, err = l.svcCtx.Store.User().FindOne(l.ctx, deviceInfo.UserId)
		if err != nil {
			l.Errorw("query user failed",
				logger.Field("user_id", deviceInfo.UserId),
				logger.Field("error", err.Error()),
			)
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user failed: %v", err.Error())
		}
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
		jwt.WithOption("LoginType", "device"),
	)
	if err != nil {
		l.Errorw("token generate error",
			logger.Field("user_id", userInfo.Id),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "token generate error: %v", err.Error())
	}

	// Store session id in redis
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err = l.svcCtx.Redis.Set(l.ctx, sessionIdCacheKey, userInfo.Id, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		l.Errorw("set session id error",
			logger.Field("user_id", userInfo.Id),
			logger.Field("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "set session id error: %v", err.Error())
	}

	loginStatus = true
	return &dto.LoginResponse{
		Token: token,
	}, nil
}

func (l *DeviceLoginLogic) registerUserAndDevice(req *dto.DeviceLoginRequest) (*user.User, error) {
	l.Infow("device not found, creating new user and device",
		logger.Field("identifier", req.Identifier),
		logger.Field("ip", req.IP),
	)

	var userInfo *user.User
	var trialSubscribe *user.Subscribe
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Create new user
		userInfo = &user.User{
			OnlyFirstPurchase: &l.svcCtx.Config.Invite.OnlyFirstPurchase,
		}
		if err := store.User().Insert(l.ctx, userInfo); err != nil {
			l.Errorw("failed to create user",
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create user failed: %v", err)
		}

		// Update refer code
		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		if err := store.User().Update(l.ctx, userInfo); err != nil {
			l.Errorw("failed to update refer code",
				logger.Field("user_id", userInfo.Id),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update refer code failed: %v", err)
		}

		// Create device auth method
		authMethod := &user.AuthMethods{
			UserId:         userInfo.Id,
			AuthType:       "device",
			AuthIdentifier: req.Identifier,
			Verified:       true,
		}
		if err := store.User().InsertUserAuthMethods(l.ctx, authMethod); err != nil {
			l.Errorw("failed to create device auth method",
				logger.Field("user_id", userInfo.Id),
				logger.Field("identifier", req.Identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create device auth method failed: %v", err)
		}

		// Insert device record
		deviceInfo := &user.Device{
			Ip:         req.IP,
			UserId:     userInfo.Id,
			UserAgent:  req.UserAgent,
			Identifier: req.Identifier,
			Enabled:    true,
			Online:     false,
		}
		if err := store.User().InsertDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to insert device",
				logger.Field("user_id", userInfo.Id),
				logger.Field("identifier", req.Identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert device failed: %v", err)
		}

		// Activate trial if enabled
		if l.svcCtx.Config.Register.EnableTrial {
			var trialErr error
			trialSubscribe, trialErr = l.activeTrial(store, userInfo.Id)
			if trialErr != nil {
				return trialErr
			}
		}

		return nil
	})

	if err != nil {
		l.Errorw("device registration failed",
			logger.Field("identifier", req.Identifier),
			logger.Field("error", err.Error()),
		)
		return nil, err
	}
	clearTrialSubscribeCache(l.ctx, l.svcCtx, trialSubscribe)

	l.Infow("device registration completed successfully",
		logger.Field("user_id", userInfo.Id),
		logger.Field("identifier", req.Identifier),
		logger.Field("refer_code", userInfo.ReferCode),
	)

	// Register log
	registerLog := log.Register{
		AuthMethod: "device",
		Identifier: req.Identifier,
		RegisterIP: req.IP,
		UserAgent:  req.UserAgent,
		Timestamp:  timeutil.Now().UnixMilli(),
	}
	content, _ := registerLog.Marshal()

	if err := l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
		Type:     log.TypeRegister.Uint8(),
		Date:     timeutil.Now().Format("2006-01-02"),
		ObjectID: userInfo.Id,
		Content:  string(content),
	}); err != nil {
		l.Errorw("failed to insert register log",
			logger.Field("user_id", userInfo.Id),
			logger.Field("ip", req.IP),
			logger.Field("error", err.Error()),
		)
	}

	return userInfo, nil
}

func (l *DeviceLoginLogic) activeTrial(store repository.Store, userId int64) (*user.Subscribe, error) {
	sub, err := store.Subscribe().FindOne(l.ctx, l.svcCtx.Config.Register.TrialSubscribe)
	if err != nil {
		l.Errorw("failed to find trial subscription template",
			logger.Field("user_id", userId),
			logger.Field("trial_subscribe_id", l.svcCtx.Config.Register.TrialSubscribe),
			logger.Field("error", err.Error()),
		)
		return nil, err
	}

	startTime := timeutil.Now()
	expireTime := tool.AddTime(l.svcCtx.Config.Register.TrialTimeUnit, l.svcCtx.Config.Register.TrialTime, startTime)
	subscribeToken := uuidx.SubscribeToken(fmt.Sprintf("Trial-%v-%s", userId, uuidx.NewUUID().String()))
	subscribeUUID := uuidx.NewUUID().String()

	userSub := &user.Subscribe{
		UserId:      userId,
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
			logger.Field("user_id", userId),
			logger.Field("error", err.Error()),
		)
		return nil, err
	}

	l.Infow("trial subscription activated successfully",
		logger.Field("user_id", userId),
		logger.Field("subscribe_id", sub.Id),
		logger.Field("expire_time", expireTime),
		logger.Field("traffic", sub.Traffic),
	)

	return userSub, nil
}
