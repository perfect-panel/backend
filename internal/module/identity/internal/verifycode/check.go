package verifycode

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CheckVerificationCode pre-checks a code without consuming it, for the UI.
// The phone number is normalized the way SendSmsCode stored it, so a number
// written with separators still matches its code.
func (s *Service) CheckVerificationCode(ctx context.Context, req *dto.CheckVerificationCodeRequest) (*dto.CheckVerificationCodeResponse, error) {
	resp := &dto.CheckVerificationCodeResponse{}
	purpose := auth.ParseVerifyType(req.Type)
	var cacheKey string
	switch req.Method {
	case identifier.Email:
		key, ok := s.emailCodeKey(purpose, req.Account)
		if !ok {
			return resp, nil
		}
		cacheKey = key
	case identifier.Mobile:
		if !identifier.CheckPhone(req.Account) {
			return nil, xerr.Errorf(xerr.TelephoneError, "invalid phone number")
		}
		phoneNumber, err := identifier.FormatToE164("", req.Account)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
		}
		cacheKey = verification.MobileCodeKey(purpose, phoneNumber)
	default:
		return resp, nil
	}
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, cacheKey, req.Code, false); err != nil {
		if errors.Is(err, verification.ErrVerificationAttemptsExceeded) {
			return nil, xerr.Wrapf(err, xerr.TooManyRequests, "verification attempts exceeded")
		}
		return resp, nil
	}
	resp.Status = true
	return resp, nil
}

// emailCodeKey is the key of the code sent to account for purpose. An
// address the sender would refuse has no code, so it reports false.
func (s *Service) emailCodeKey(purpose auth.VerifyType, account string) (string, bool) {
	cfg := s.deps.Config()
	email, err := identifier.ValidateEmail(account, cfg.DomainSuffixList, purpose == auth.Register && cfg.EnableDomainSuffix)
	if err != nil {
		return "", false
	}
	return verification.EmailCodeKey(purpose, email), true
}
