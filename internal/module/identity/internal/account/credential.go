package account

import (
	"context"
	"sort"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
)

// The credential changes the login audit records, as the method of a
// successful entry: a reset proven by a code sent to a bound address, and a
// change proven by the current password or a bound address.
const (
	PasswordReset  = "password_reset"
	PasswordChange = "password_change"
)

// ThirdPartyBindings returns, sorted, the types of the sign-in methods bound
// to an account that a password change does not touch: the OAuth providers
// and Telegram. Email, phone number and device bindings are the account's
// own addresses and devices, not third parties.
func ThirdPartyBindings(methods []*user.AuthMethods) []string {
	types := make([]string, 0, len(methods))
	for _, method := range methods {
		if method == nil {
			continue
		}
		switch method.AuthType {
		case identifier.Email, identifier.Mobile, identifier.Device:
			continue
		}
		types = append(types, method.AuthType)
	}
	sort.Strings(types)
	return types
}

// RecordCredentialChange audits a password change of the account userID as a
// successful login-log entry whose method is kind, so the account's login
// history shows when and from where its password changed. Without an audit
// log wired (a test double) nothing is recorded.
func RecordCredentialChange(ctx context.Context, logs repository.LogRepo, userID int64, kind string) error {
	if logs == nil {
		return nil
	}
	return RecordLogin(ctx, logs, userID, kind, true)
}

// NotifyPasswordChanged tells the account, through notify, that its password
// changed and which third-party bindings stay. The notice is best effort: a
// failure is logged and the change stands. A nil notify sends nothing.
func NotifyPasswordChanged(ctx context.Context, notify func(context.Context, int64, []string) error, userID int64, bindings []string) {
	if notify == nil {
		return
	}
	if err := notify(ctx, userID, bindings); err != nil {
		logger.WithContext(ctx).Infow("password change notice not delivered",
			logger.Field("user_id", userID), logger.Field("error", err.Error()))
	}
}
