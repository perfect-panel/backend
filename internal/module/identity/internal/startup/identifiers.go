package startup

import (
	"context"

	"github.com/perfect-panel/backend/pkg/logger"
)

// ValidateEmailIdentities fails with EmailIdentityAmbiguous when two email
// bindings differ only in letter case or surrounding spaces: email sign-in and
// password resets would match both, so the server refuses to start until an
// operator resolved them. Other failures are the repository's, as it
// reported them.
func (s *Service) ValidateEmailIdentities(ctx context.Context) error {
	return s.deps.UserAuths.ValidateEmailIdentityUniqueness(ctx)
}

// NormalizePhoneNumbers rewrites the phone numbers stored in another form
// than E.164 (the admin panel used to store "<area>-<number>") to E.164, the
// form sign-in and password resets look them up in, and logs what it did. It
// is idempotent: a number whose E.164 form another binding holds, or one that
// does not parse, is left as it is and reported. A failure is the
// repository's, as it reported it.
func (s *Service) NormalizePhoneNumbers(ctx context.Context) error {
	result, err := s.deps.UserAuths.NormalizeMobileIdentifiers(ctx)
	if err != nil {
		return err
	}
	if result.Converted > 0 || result.Conflicts > 0 || result.Unparsable > 0 {
		logger.WithContext(ctx).Infow("[Identity] normalized stored phone numbers to E.164",
			logger.Field("converted", result.Converted),
			logger.Field("conflicts", result.Conflicts),
			logger.Field("unparsable", result.Unparsable))
	}
	return nil
}
