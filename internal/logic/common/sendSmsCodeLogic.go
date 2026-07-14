package common

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type SmsSendCount struct {
	Count    int64 `json:"count"`
	CreateAt int64 `json:"create_at"`
}

type SendSmsCodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewSendSmsCodeLogic Get sms verification code
func NewSendSmsCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendSmsCodeLogic {
	return &SendSmsCodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SendSmsCodeLogic) SendSmsCode(req *dto.SendSmsCodeRequest) (resp *dto.SendCodeResponse, err error) {
	phoneNumber, err := phone.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
	}

	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.ParseVerifyType(req.Type), phoneNumber)
	// Check if the limit is exceeded of current request
	limiter := limit.NewPeriodLimit(60, 1, l.svcCtx.Redis, fmt.Sprintf("%s:%s:%s", config.SendIntervalKeyPrefix, "mobile", constant.ParseVerifyType(req.Type)))
	permit, err := limiter.Take(phoneNumber)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to take limit")
	}
	if !limiter.ParsePermitState(permit) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "send sms too many requests")
	}
	// Check if the limit is exceeded of the today
	permit, err = l.svcCtx.AuthLimiter.Take(fmt.Sprintf("%s:%s:%s", "mobile", constant.ParseVerifyType(req.Type), phoneNumber))
	if err != nil {
		return nil, err
	}
	if !l.svcCtx.AuthLimiter.ParsePermitState(permit) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TodaySendCountExceedsLimit), "This account has reached the limit of sending times today")
	}
	m, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "mobile", phoneNumber)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if constant.ParseVerifyType(req.Type) == constant.Register && m.Id > 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "mobile already bind")
	} else if constant.ParseVerifyType(req.Type) == constant.Security && m.Id == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "mobile not bind")
	}

	taskPayload := queue.SendSmsPayload{
		Type:          req.Type,
		Telephone:     req.Telephone,
		TelephoneArea: req.TelephoneAreaCode,
	}
	// Generate verification code
	code := random.Key(6, 0)
	taskPayload.Telephone = req.Telephone
	taskPayload.Content = code
	// Save to Redis
	payload := CacheKeyPayload{
		Code:   code,
		LastAt: timeutil.Now().Unix(),
	}
	// Marshal the payload
	val, _ := json.Marshal(payload)
	if err = l.svcCtx.Redis.Set(l.ctx, cacheKey, string(val), time.Second*time.Duration(l.svcCtx.Config.VerifyCode.ExpireTime)).Err(); err != nil {
		l.Errorw("[SendSmsCode]: Redis Error", logger.Field("error", err.Error()), logger.Field("cacheKey", cacheKey))
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), "Failed to set verification code")
	}

	// Marshal the task payload
	payloadValue, err := json.Marshal(taskPayload)
	if err != nil {
		l.Errorw("[SendSmsCode]: Marshal Error", logger.Field("error", err.Error()))
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), "Failed to marshal task payload")
	}
	// Create a queue task
	task := asynq.NewTask(queue.ForthwithSendSms, payloadValue)
	// Enqueue the task
	taskInfo, err := l.svcCtx.Queue.Enqueue(task)
	if err != nil {
		l.Errorw("[SendSmsCode]: Enqueue Error", logger.Field("error", err.Error()), logger.Field("payload", string(payloadValue)))
		return nil, errors.Wrap(xerr.NewErrCode(xerr.ERROR), "Failed to enqueue task")
	}
	l.Infow("[SendSmsCode]: Enqueue Success", logger.Field("taskID", taskInfo.ID), logger.Field("payload", string(payloadValue)))
	if l.svcCtx.Config.Model == constant.DevMode {
		return &dto.SendCodeResponse{
			Code:   taskPayload.Content,
			Status: true,
		}, nil
	}
	return &dto.SendCodeResponse{
		Status: true,
	}, nil
}
