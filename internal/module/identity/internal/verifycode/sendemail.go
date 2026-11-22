package verifycode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/auth/identifier"
	"github.com/perfect-panel/server/internal/auth/ratelimit"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/infra/mail"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	dto "github.com/perfect-panel/server/internal/module/identity/contract"
	"github.com/perfect-panel/server/internal/module/identity/entity/auth"
	"github.com/perfect-panel/server/internal/module/identity/internal/verification"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"github.com/perfect-panel/server/pkg/xerr"
	"gorm.io/gorm"
)

// IntervalTime is the default number of seconds between two codes sent to
// one address.
const IntervalTime = 60

// SendEmailCode sends a verification code to an email address: a register
// code to an address no account has, a security code to one an account has.
func (s *Service) SendEmailCode(ctx context.Context, req *dto.SendCodeRequest) (*dto.SendCodeResponse, error) {
	cfg := s.deps.Config()
	verifyType := auth.ParseVerifyType(req.Type)
	email, err := identifier.ValidateEmail(
		req.Email,
		cfg.DomainSuffixList,
		verifyType == auth.Register && cfg.EnableDomainSuffix,
	)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid email")
	}
	if err := s.ensureCodeAllowed(ctx, verifyType, identifier.Email); err != nil {
		return nil, err
	}
	if err := s.verifyHuman(ctx, verifyType, req.CfToken); err != nil {
		return nil, err
	}
	if err := s.takeIPPermit(ctx); err != nil {
		return nil, err
	}
	cacheKey := verification.EmailCodeKey(verifyType, email)
	interval := cfg.VerifyCodeInterval
	if interval <= 0 {
		interval = IntervalTime
	}
	limiter := ratelimit.NewPeriodLimit(int(interval), 1, s.deps.Redis, fmt.Sprintf("%semail:%s:", config.SendIntervalKeyPrefix, verifyType))
	permit, err := limiter.Take(ctx, email)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "take the send interval permit")
	}
	if !limiter.ParsePermitState(permit) {
		return nil, xerr.Errorf(xerr.TooManyRequests, "send email too many requests")
	}
	dailyLimit := cfg.VerifyCodeLimit
	if dailyLimit <= 0 {
		dailyLimit = 15
	}
	dailyLimiter := ratelimit.NewPeriodLimit(86400, int(dailyLimit), s.deps.Redis, config.SendCountLimitKeyPrefix, ratelimit.Align())
	permit, err = dailyLimiter.Take(ctx, fmt.Sprintf("%s:%s:%s", "email", verifyType, email))
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "take the daily send permit")
	}
	if !dailyLimiter.ParsePermitState(permit) {
		return nil, xerr.Errorf(xerr.TodaySendCountExceedsLimit, "send email too many requests today")
	}
	m, err := s.deps.Store.UserAuth().FindUserAuthMethodByOpenID(ctx, identifier.Email, email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find email identity")
	}
	if verifyType == auth.Register && m.Id > 0 {
		return nil, xerr.Errorf(xerr.UserExist, "email already bound")
	} else if verifyType == auth.Security && m.Id == 0 {
		return nil, xerr.Errorf(xerr.UserNotExist, "email not bound")
	}

	var taskPayload taskqueue.SendEmailPayload
	taskPayload.Metadata, _ = requestmeta.From(ctx)
	code := random.Key(6, 0)
	expireSeconds := cfg.VerifyCodeExpire
	if expireSeconds <= 0 {
		expireSeconds = IntervalTime * 5
	}
	taskPayload.Type = taskqueue.EmailTypeVerify
	taskPayload.Email = email
	taskPayload.Subject = mail.DefaultEmailVerifySubject
	taskPayload.Content = map[string]any{
		"Type":     req.Type,
		"SiteLogo": cfg.SiteLogo,
		"SiteName": cfg.SiteName,
		"Expire":   (expireSeconds + 59) / 60,
		"Code":     code,
	}
	expiration := time.Duration(expireSeconds) * time.Second
	if err = verification.SaveVerificationCode(ctx, s.deps.Redis, cacheKey, code, expiration); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "store verification code")
	}

	payload, err := json.Marshal(taskPayload)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "marshal task payload")
	}
	taskInfo, err := s.deps.Queue.EnqueueContext(ctx, asynq.NewTask(taskqueue.ForthwithSendEmail, payload), asynq.MaxRetry(3))
	if err != nil {
		_ = verification.DeleteVerificationCode(ctx, s.deps.Redis, cacheKey)
		return nil, xerr.Wrapf(err, xerr.ERROR, "enqueue verification email")
	}
	logger.WithContext(ctx).Infow("[SendEmailCode]: Enqueue Success", logger.Field("taskID", taskInfo.ID), logger.Field("type", taskPayload.Type))
	return &dto.SendCodeResponse{Status: true}, nil
}
