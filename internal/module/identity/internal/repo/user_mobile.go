package repo

import (
	"context"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"gorm.io/gorm"
)

// NormalizeMobileIdentifiers converts phone numbers stored in another form
// than E.164 to E.164, the form every sign-in, reset and verification code
// looks them up in. The admin panel used to store "<area>-<number>", which
// left those accounts unable to sign in or reset by phone. Only a stored form
// that names its country is converted (see legacyMobileE164): a number in
// national form, such as the Chinese "15012345678", is a valid number of
// another country once "+" is put in front of it, and converting it would
// hand the account's SMS password reset to that number's holder. Such rows,
// and rows whose number is not valid, are left as they are and reported for
// the operator with the number masked. Formatting needs libphonenumber, so
// this runs at startup rather than as a SQL migration; a run over converted
// data changes nothing.
//
// A number whose E.164 form another binding already holds is left as it is
// and logged: it cannot take the number from the other binding, and the
// owner of that binding signs in with it already.
func (m *UserRepo) NormalizeMobileIdentifiers(ctx context.Context) (repository.MobileNormalization, error) {
	var result repository.MobileNormalization
	var candidates []*user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &candidates, func(conn *gorm.DB, v any) error {
		return legacyMobileCandidates(conn).Order("id").Find(v).Error
	})
	if err != nil {
		return result, err
	}
	log := logger.WithContext(ctx)
	for _, method := range candidates {
		canonical, ok := legacyMobileE164(method.AuthIdentifier)
		if !ok {
			result.Unparsable++
			log.Errorw("[NormalizeMobileIdentifiers] stored phone number names no country or is not a valid number; left unchanged",
				logger.Field("auth_method_id", method.Id), logger.Field("user_id", method.UserId),
				logger.Field("number", maskStoredIdentifier(method.AuthIdentifier)))
			continue
		}
		if canonical == method.AuthIdentifier {
			continue
		}
		var holders []user.AuthMethods
		err = m.QueryNoCacheCtx(ctx, &holders, func(conn *gorm.DB, v any) error {
			return queryAuthMethodsByExactIdentifier(conn, identifier.Mobile, canonical).Limit(1).Find(v).Error
		})
		if err != nil {
			return result, err
		}
		if len(holders) > 0 {
			result.Conflicts++
			log.Errorw("[NormalizeMobileIdentifiers] E.164 form already belongs to another binding; left unchanged",
				logger.Field("auth_method_id", method.Id), logger.Field("user_id", method.UserId),
				logger.Field("holder_auth_method_id", holders[0].Id), logger.Field("holder_user_id", holders[0].UserId),
				logger.Field("number", identifier.MaskPhoneNumber(canonical)))
			continue
		}
		converted := false
		err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
			// The stored value is part of the condition, so a concurrent
			// startup converting the same row changes nothing twice.
			update := conn.Model(&user.AuthMethods{}).
				Where("id = ? AND auth_identifier = ?", method.Id, method.AuthIdentifier).
				Update("auth_identifier", canonical)
			converted = update.RowsAffected == 1
			return update.Error
		}, method.GetCacheKeys()...)
		if err != nil {
			// A concurrent binding of the same number wins the unique index.
			result.Conflicts++
			log.Errorw("[NormalizeMobileIdentifiers] convert phone number failed; left unchanged",
				logger.Field("auth_method_id", method.Id), logger.Field("user_id", method.UserId), logger.Field("error", err.Error()))
			continue
		}
		if converted {
			result.Converted++
		}
	}
	return result, nil
}

// mobileSeparators are the characters people put between the digits of a
// phone number. legacyMobileE164 drops them, and legacyMobileCandidates
// selects the stored numbers holding one.
const mobileSeparators = " -.()"

// legacyMobileCandidates selects the mobile bindings whose stored form is
// not E.164, "+" followed by digits: the ones without the "+" and the ones
// with a separator.
func legacyMobileCandidates(conn *gorm.DB) *gorm.DB {
	conditions := []string{"auth_identifier NOT LIKE ?"}
	args := []any{"+%"}
	for _, separator := range mobileSeparators {
		conditions = append(conditions, "auth_identifier LIKE ?")
		args = append(args, "%"+string(separator)+"%")
	}
	return conn.Model(&user.AuthMethods{}).
		Where("auth_type = ?", identifier.Mobile).
		Where("("+strings.Join(conditions, " OR ")+")", args...)
}

// legacyMobileE164 returns the E.164 form of a stored phone number whose
// form names its country: "<country code>-<number>", the form the admin
// panel used to store, or "+" followed by the digits with separators between
// them. The country code is read from the stored value, never assumed, and
// the result must be a number libphonenumber knows as valid; a national form
// or a value that is not a number has no E.164 form and reports false.
func legacyMobileE164(stored string) (string, bool) {
	digits, ok := strings.CutPrefix(strings.TrimSpace(stored), "+")
	if !ok {
		countryCode, national, dashed := strings.Cut(stored, "-")
		countryCode = strings.TrimSpace(countryCode)
		if !dashed || !isDigits(countryCode) {
			return "", false
		}
		digits = countryCode + national
	}
	digits = strings.Map(func(r rune) rune {
		if strings.ContainsRune(mobileSeparators, r) {
			return -1
		}
		return r
	}, digits)
	if !isDigits(digits) {
		return "", false
	}
	parsed, err := phonenumbers.Parse("+"+digits, "")
	if err != nil || !phonenumbers.IsValidNumber(parsed) {
		return "", false
	}
	return phonenumbers.Format(parsed, phonenumbers.E164), true
}

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// maskStoredIdentifier hides a stored identifier for a log line, keeping its
// first three and last two characters, so an operator can tell the reported
// rows apart without the log holding the numbers themselves.
func maskStoredIdentifier(value string) string {
	const keepLeading, keepTrailing = 3, 2
	runes := []rune(value)
	if len(runes) <= keepLeading+keepTrailing {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:keepLeading]) + strings.Repeat("*", len(runes)-keepLeading-keepTrailing) + string(runes[len(runes)-keepTrailing:])
}
