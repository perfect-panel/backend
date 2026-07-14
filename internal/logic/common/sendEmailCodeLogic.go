package common

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
)

type SendEmailCodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

const (
	IntervalTime = 60
)

type VerifyTemplate struct {
	Type     uint8
	SiteLogo string
	SiteName string
	Expire   uint8
	Code     string
}
type CacheKeyPayload struct {
	Code   string `json:"code"`
	LastAt int64  `json:"lastAt"`
}

// NewSendEmailCodeLogic Get verification code
func NewSendEmailCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendEmailCodeLogic {
	return &SendEmailCodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SendEmailCodeLogic) SendEmailCode(req *dto.SendCodeRequest) (resp *dto.SendCodeResponse, err error) {
	email := authmethod.CanonicalEmail(req.Email)
	// Check if there is Redis in the code
	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.ParseVerifyType(req.Type), email)
	// Check if the limit is exceeded of current request
	limiter := limit.NewPeriodLimit(60, 1, l.svcCtx.Redis, fmt.Sprintf("%s:%s:%s", config.SendIntervalKeyPrefix, "email", constant.ParseVerifyType(req.Type)))
	permit, err := limiter.Take(email)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to take limit")
	}
	if !limiter.ParsePermitState(permit) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "send email too many requests")
	}
	// Check if the limit is exceeded of today
	permit, err = l.svcCtx.AuthLimiter.Take(fmt.Sprintf("%s:%s:%s", "email", constant.ParseVerifyType(req.Type), email))
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to take limit")
	}
	if !l.svcCtx.AuthLimiter.ParsePermitState(permit) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TodaySendCountExceedsLimit), "send email too many requests")
	}
	m, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, authmethod.Email, email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if constant.ParseVerifyType(req.Type) == constant.Register && m.Id > 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "mobile already bind")
	} else if constant.ParseVerifyType(req.Type) == constant.Security && m.Id == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "mobile not bind")
	}

	var payload CacheKeyPayload
	var taskPayload queue.SendEmailPayload
	// Generate verification code
	code := random.Key(6, 0)
	taskPayload.Type = queue.EmailTypeVerify
	taskPayload.Email = email
	taskPayload.Subject = "Verification code"
	taskPayload.Content = map[string]interface{}{
		"Type":     req.Type,
		"SiteLogo": l.svcCtx.Config.Site.SiteLogo,
		"SiteName": l.svcCtx.Config.Site.SiteName,
		"Expire":   5,
		"Code":     code,
	}
	// Save to Redis
	payload = CacheKeyPayload{
		Code:   code,
		LastAt: timeutil.Now().Unix(),
	}
	// Marshal the payload
	val, _ := json.Marshal(payload)
	if err = l.svcCtx.Redis.Set(l.ctx, cacheKey, string(val), time.Second*IntervalTime*5).Err(); err != nil {
		l.Errorw("[SendEmailCode]: Redis Error", logger.Field("error", err.Error()), logger.Field("cacheKey", cacheKey))
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), "Failed to set verification code")
	}

	// Marshal the task payload
	payloadBuy, err := json.Marshal(taskPayload)
	if err != nil {
		l.Errorw("[SendEmailCode]: Marshal Error", logger.Field("error", err.Error()))
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), "Failed to marshal task payload")
	}
	// Create a queue task
	task := asynq.NewTask(queue.ForthwithSendEmail, payloadBuy, asynq.MaxRetry(3))
	// Enqueue the task
	taskInfo, err := l.svcCtx.Queue.Enqueue(task)
	if err != nil {
		l.Errorw("[SendEmailCode]: Enqueue Error", logger.Field("error", err.Error()), logger.Field("type", taskPayload.Type))
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), "Failed to enqueue task")
	}
	l.Infow("[SendEmailCode]: Enqueue Success", logger.Field("taskID", taskInfo.ID), logger.Field("type", taskPayload.Type))
	if l.svcCtx.Config.Model == constant.DevMode {
		return &dto.SendCodeResponse{
			Code:   payload.Code,
			Status: true,
		}, nil
	} else {
		return &dto.SendCodeResponse{
			Status: true,
		}, nil
	}
}
