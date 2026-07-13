package repository

import (
	"context"
	"errors"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/authmethod"
	"gorm.io/gorm"
)

var ErrAmbiguousEmailIdentity = errors.New("ambiguous email identity")
var ErrInvalidEmailIdentity = errors.New("invalid email identity")

func findUserAuthMethodByIdentifier(conn *gorm.DB, authType, identifier string) (*user.AuthMethods, error) {
	canonicalIdentifier, err := canonicalAuthIdentifier(authType, identifier)
	if err != nil {
		return nil, err
	}

	var data user.AuthMethods
	err = queryAuthMethodsByExactIdentifier(conn, authType, canonicalIdentifier).First(&data).Error
	if authType != authmethod.Email || err == nil {
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

func canonicalAuthIdentifier(authType, identifier string) (string, error) {
	canonicalIdentifier := authmethod.CanonicalIdentifier(authType, identifier)
	if authType == authmethod.Email && canonicalIdentifier == "" {
		return "", ErrInvalidEmailIdentity
	}
	return canonicalIdentifier, nil
}

func queryAuthMethodsByExactIdentifier(conn *gorm.DB, authType, identifier string) *gorm.DB {
	return conn.Model(&user.AuthMethods{}).Where("auth_type = ? AND auth_identifier = ?", authType, identifier)
}

func queryFoldedEmailAuthMethods(conn *gorm.DB, canonicalEmail string) *gorm.DB {
	return conn.Model(&user.AuthMethods{}).
		Where("auth_type = ? AND LOWER(TRIM(auth_identifier)) = ?", authmethod.Email, canonicalEmail).
		Limit(2)
}

func emailIdentityCollisionQuery(conn *gorm.DB) *gorm.DB {
	return conn.Model(&user.AuthMethods{}).
		Select("LOWER(TRIM(auth_identifier)) AS auth_identifier").
		Where("auth_type = ?", authmethod.Email).
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
	if authMethod.AuthType != authmethod.Email {
		return nil
	}

	var methods []user.AuthMethods
	if err := emailWriteCollisionQuery(conn, authMethod.AuthIdentifier, authMethod.Id).Find(&methods).Error; err != nil {
		return err
	}
	if hasConflictingEmailIdentity(authMethod.Id, methods) {
		return ErrAmbiguousEmailIdentity
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
		return nil, ErrAmbiguousEmailIdentity
	}
}

func (m *userRepo) ValidateEmailIdentityUniqueness(ctx context.Context) error {
	var collisions []struct {
		AuthIdentifier string
	}
	err := m.QueryNoCacheCtx(ctx, &collisions, func(conn *gorm.DB, _ interface{}) error {
		return emailIdentityCollisionQuery(conn).Find(&collisions).Error
	})
	if err != nil {
		return err
	}
	if len(collisions) > 0 {
		return ErrAmbiguousEmailIdentity
	}
	return nil
}
