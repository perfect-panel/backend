package repo

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The identity errors the repository reports. Each is returned with the
// identity code a client receives for it, so a caller wrapping it under a
// generic database code still reports the specific failure.
var (
	// ErrAmbiguousEmailIdentity reports an email address that folds onto
	// more than one binding.
	ErrAmbiguousEmailIdentity = errors.New("ambiguous email identity")
	// ErrInvalidEmailIdentity reports an empty email address.
	ErrInvalidEmailIdentity = errors.New("invalid email identity")
	// ErrInvalidMobileIdentity reports a phone number that does not parse,
	// so it has no E.164 form to be stored in.
	ErrInvalidMobileIdentity = errors.New("invalid mobile identity")
)

func ambiguousEmail() error {
	return xerr.Wrapf(ErrAmbiguousEmailIdentity, xerr.EmailIdentityAmbiguous, "resolve email identity")
}

func findUserAuthMethodByIdentifier(conn *gorm.DB, authType, authIdentifier string) (*user.AuthMethods, error) {
	canonicalIdentifier, err := lookupIdentifier(authType, authIdentifier)
	if err != nil {
		return nil, err
	}

	var data user.AuthMethods
	err = queryAuthMethodsByExactIdentifier(conn, authType, canonicalIdentifier).First(&data).Error
	if authType != identifier.Email || err == nil {
		return &data, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var methods []user.AuthMethods
	if err := queryFoldedEmailAuthMethods(conn, canonicalIdentifier).Find(&methods).Error; err != nil {
		return nil, err
	}
	return resolveUniqueAuthMethod(methods)
}

// storedIdentifier normalizes an identifier being written: see
// identifier.NormalizeIdentifier. An email address or phone number that
// cannot be normalized is refused.
func storedIdentifier(authType, authIdentifier string) (string, error) {
	canonical, err := identifier.NormalizeIdentifier(authType, authIdentifier)
	switch {
	case err == nil:
		return canonical, nil
	case authType == identifier.Email:
		return "", xerr.Wrapf(ErrInvalidEmailIdentity, xerr.InvalidParams, "normalize email identity: %v", err)
	default:
		return "", xerr.Wrapf(ErrInvalidMobileIdentity, xerr.TelephoneError, "normalize mobile identity: %v", err)
	}
}

// lookupIdentifier normalizes an identifier being looked up. A phone number
// that cannot be normalized cannot have been stored, so it is looked up as
// given and simply matches nothing.
func lookupIdentifier(authType, authIdentifier string) (string, error) {
	if authType == identifier.Mobile {
		return identifier.CanonicalIdentifier(authType, authIdentifier), nil
	}
	return storedIdentifier(authType, authIdentifier)
}

func queryAuthMethodsByExactIdentifier(conn *gorm.DB, authType, authIdentifier string) *gorm.DB {
	return conn.Model(&user.AuthMethods{}).Where("auth_type = ? AND auth_identifier = ?", authType, authIdentifier)
}

func queryFoldedEmailAuthMethods(conn *gorm.DB, canonicalEmail string) *gorm.DB {
	return conn.Model(&user.AuthMethods{}).
		Where("auth_type = ? AND LOWER(TRIM(auth_identifier)) = ?", identifier.Email, canonicalEmail).
		Limit(2)
}

func emailIdentityCollisionQuery(conn *gorm.DB) *gorm.DB {
	return conn.Model(&user.AuthMethods{}).
		Select("LOWER(TRIM(auth_identifier)) AS auth_identifier").
		Where("auth_type = ?", identifier.Email).
		Group("LOWER(TRIM(auth_identifier))").
		Having("COUNT(*) > 1").
		Limit(1)
}

func emailWriteCollisionQuery(conn *gorm.DB, canonicalEmail string, currentID int64) *gorm.DB {
	query := queryFoldedEmailAuthMethods(conn, canonicalEmail)
	if currentID != 0 {
		query = query.Where("id <> ?", currentID)
	}
	return query.Limit(1)
}

func hasConflictingEmailIdentity(currentID int64, methods []user.AuthMethods) bool {
	for _, method := range methods {
		if method.Id != currentID {
			return true
		}
	}
	return false
}

func guardEmailIdentityWrite(conn *gorm.DB, authMethod *user.AuthMethods) error {
	if authMethod.AuthType != identifier.Email {
		return nil
	}

	var methods []user.AuthMethods
	if err := emailWriteCollisionQuery(conn, authMethod.AuthIdentifier, authMethod.Id).Find(&methods).Error; err != nil {
		return err
	}
	if hasConflictingEmailIdentity(authMethod.Id, methods) {
		return ambiguousEmail()
	}
	return nil
}

func resolveUniqueAuthMethod(methods []user.AuthMethods) (*user.AuthMethods, error) {
	switch len(methods) {
	case 0:
		return nil, gorm.ErrRecordNotFound
	case 1:
		return &methods[0], nil
	default:
		return nil, ambiguousEmail()
	}
}

func (m *UserRepo) ValidateEmailIdentityUniqueness(ctx context.Context) error {
	var collisions []struct {
		AuthIdentifier string
	}
	err := m.QueryNoCacheCtx(ctx, &collisions, func(conn *gorm.DB, _ any) error {
		return emailIdentityCollisionQuery(conn).Find(&collisions).Error
	})
	if err != nil {
		return err
	}
	if len(collisions) > 0 {
		return ambiguousEmail()
	}
	return nil
}
