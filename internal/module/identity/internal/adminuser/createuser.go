package adminuser

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// CreateUser creates an account on the administrator's behalf. Its phone
// number is stored in E.164 like every self-service one, so the account signs
// in and resets by phone; an identifier another account holds is refused.
func (s *Service) CreateUser(ctx context.Context, req *dto.CreateUserRequest) error {
	if err := validateReferralPercentage(req.ReferralPercentage); err != nil {
		return err
	}
	referCode := req.ReferCode
	if referCode == "" {
		// The account has no id yet, so the code is derived from the time.
		referCode = user.GenerateInviteCode(timeutil.Now().UnixMicro())
	}
	plain := req.Password
	if plain == "" {
		// Without a password the account signs in only after a password
		// reset or through another method. Its identifiers are known to
		// anyone who knows the account, so none of them can be the password.
		var err error
		if plain, err = unknownPassword(); err != nil {
			return err
		}
	}
	newUser := &user.User{
		Password:           password.EncodePassWord(plain),
		Algo:               password.PasswordAlgoArgon2id,
		ReferralPercentage: req.ReferralPercentage,
		OnlyFirstPurchase:  &req.OnlyFirstPurchase,
		ReferCode:          referCode,
		IsAdmin:            &req.IsAdmin,
	}

	var identities []user.AuthMethods
	if req.TelephoneAreaCode != "" && req.Telephone != "" {
		phone, err := identifier.FormatToE164(req.TelephoneAreaCode, req.Telephone)
		if err != nil {
			return xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
		}
		if err := s.ensureIdentityFree(ctx, identifier.Mobile, phone, xerr.TelephoneExist); err != nil {
			return err
		}
		identities = append(identities, user.AuthMethods{AuthType: identifier.Mobile, AuthIdentifier: phone})
	}
	if req.Email != "" {
		if err := s.ensureIdentityFree(ctx, identifier.Email, req.Email, xerr.EmailExist); err != nil {
			return err
		}
		identities = append(identities, user.AuthMethods{AuthType: identifier.Email, AuthIdentifier: req.Email})
	}

	if req.RefererUser != "" {
		referer, err := s.deps.Users.FindOneByEmail(ctx, req.RefererUser)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return xerr.Errorf(xerr.UserNotExist, "referer user not found")
			}
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find referer user")
		}
		newUser.RefererId = referer.Id
	}

	// Two sequential domain transactions replace the old cross-domain one:
	// the identity transaction creates the account; the billing module's
	// own transaction credits the initial money. A failure between them
	// leaves an uncredited account the admin can adjust — the same
	// partial-failure surface the flows will have as services.
	if err := s.deps.Store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		return account.Create(ctx, tx, account.New{User: newUser, Identities: identities})
	}); err != nil {
		return err
	}
	if req.Balance == 0 && req.Commission == 0 && req.GiftAmount == 0 {
		return nil
	}
	return s.deps.Wallet.OpenWallet(ctx, wallet.Wallet{
		UserId:     newUser.Id,
		Balance:    req.Balance,
		GiftAmount: req.GiftAmount,
		Commission: req.Commission,
	})
}

// unknownPassword returns a password nobody knows, 256 random bits, for an
// account that must not sign in with a password until one is set.
func unknownPassword() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", xerr.Wrapf(err, xerr.ERROR, "generate the account's password")
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

// ensureIdentityFree refuses an identifier another account holds with the
// taken code; a failed lookup is a database error, not a free identifier.
func (s *Service) ensureIdentityFree(ctx context.Context, authType, authIdentifier string, taken uint32) error {
	_, err := s.deps.UserAuths.FindUserAuthMethodByOpenID(ctx, authType, authIdentifier)
	switch {
	case err == nil:
		return xerr.Errorf(taken, "the %s identifier is bound to an account", authType)
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil
	default:
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity", authType)
	}
}
