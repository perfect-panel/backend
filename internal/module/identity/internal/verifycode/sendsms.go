package verifycode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// SendSmsCode sends a verification code to a phone number: a register code
// to a number no account has, a security code to one an account has. The
// code is stored under the number's E.164 form, the one every checker uses.
func (s *Service) SendSmsCode(ctx context.Context, req *dto.SendSmsCodeRequest) (*dto.SendCodeResponse, error) {
	cfg := s.deps.Config()
	verifyType := auth.ParseVerifyType(req.Type)
	if err := s.ensureCodeAllowed(ctx, verifyType, identifier.Mobile); err != nil {
		return nil, err
	}
	if err := s.verifyHuman(ctx, verifyType, req.CfToken); err != nil {
		return nil, err
	}
	// Each code costs the operator money; outside the configured countries a
	// script could pump premium-rate numbers.
	if cfg.MobileWhitelistEnabled && !areaCodeAllowed(req.TelephoneAreaCode, cfg.MobileWhitelist) {
		return nil, xerr.Errorf(xerr.TelephoneError, "area code %q is not allowed", req.TelephoneAreaCode)
	}
	phoneNumber, err := identifier.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
	}
	if err := s.takeIPPermit(ctx); err != nil {
		return nil, err
	}

	cacheKey := verification.MobileCodeKey(verifyType, phoneNumber)
	interval := cfg.VerifyCodeInterval
	if interval <= 0 {
		interval = 60
	}
	limiter := ratelimit.NewPeriodLimit(int(interval), 1, s.deps.Redis, fmt.Sprintf("%smobile:%s:", config.SendIntervalKeyPrefix, verifyType))
	permit, err := limiter.Take(ctx, phoneNumber)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "take the send interval permit")
	}
	if !limiter.ParsePermitState(permit) {
		return nil, xerr.Errorf(xerr.TooManyRequests, "send sms too many requests")
	}
	dailyLimit := cfg.VerifyCodeLimit
	if dailyLimit <= 0 {
		dailyLimit = 15
	}
	dailyLimiter := ratelimit.NewPeriodLimit(86400, int(dailyLimit), s.deps.Redis, config.SendCountLimitKeyPrefix, ratelimit.Align())
	permit, err = dailyLimiter.Take(ctx, fmt.Sprintf("%s:%s:%s", "mobile", verifyType, phoneNumber))
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "take the daily send permit")
	}
	if !dailyLimiter.ParsePermitState(permit) {
		return nil, xerr.Errorf(xerr.TodaySendCountExceedsLimit, "this account has reached the limit of sending times today")
	}
	m, err := s.deps.Store.UserAuth().FindUserAuthMethodByOpenID(ctx, identifier.Mobile, phoneNumber)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find mobile identity")
	}
	if verifyType == auth.Register && m.Id > 0 {
		return nil, xerr.Errorf(xerr.UserExist, "mobile already bound")
	} else if verifyType == auth.Security && m.Id == 0 {
		return nil, xerr.Errorf(xerr.UserNotExist, "mobile not bound")
	}

	metadata, _ := requestmeta.From(ctx)
	code := random.Key(6, 0)
	taskPayload := taskqueue.SendSmsPayload{
		Metadata:      metadata,
		Type:          req.Type,
		Telephone:     req.Telephone,
		TelephoneArea: req.TelephoneAreaCode,
		Content:       code,
	}
	if err = verification.SaveVerificationCode(ctx, s.deps.Redis, cacheKey, code, time.Second*time.Duration(cfg.VerifyCodeExpire)); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "store verification code")
	}

	payload, err := json.Marshal(taskPayload)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "marshal task payload")
	}
	taskInfo, err := s.deps.Queue.EnqueueContext(ctx, asynq.NewTask(taskqueue.ForthwithSendSms, payload))
	if err != nil {
		_ = verification.DeleteVerificationCode(ctx, s.deps.Redis, cacheKey)
		return nil, xerr.Wrapf(err, xerr.ERROR, "enqueue verification sms")
	}
	logger.WithContext(ctx).Infow("[SendSmsCode]: Enqueue Success", logger.Field("taskID", taskInfo.ID), logger.Field("type", taskPayload.Type))
	return &dto.SendCodeResponse{Status: true}, nil
}

// areaCodeAllowed reports whether the area code is on the whitelist; either
// side may carry a leading plus.
func areaCodeAllowed(areaCode string, whitelist []string) bool {
	areaCode = strings.TrimPrefix(strings.TrimSpace(areaCode), "+")
	for _, allowed := range whitelist {
		if strings.TrimPrefix(strings.TrimSpace(allowed), "+") == areaCode {
			return true
		}
	}
	return false
}
