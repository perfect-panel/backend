// Package account holds what every account-creating and sign-in flow of the
// identity module shares: creating an account with its identities, the
// registration event and audit, the login audit, the account state check and
// the session a sign-in ends with.
package account

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// RegisteredTopic is the event every self-service registration emits; the
// subscription module grants the trial when it consumes it.
const RegisteredTopic = "identity.user_registered"

// New is an account to create.
type New struct {
	// User is the row to insert: password, referer and flags. Create sets
	// its Id and, unless one is given, its refer code.
	User *user.User
	// Identities are the sign-in identities created with the account.
	Identities []user.AuthMethods
	// Device is the device row created with the account, if any.
	Device *user.Device
}

// Create writes the account inside tx: the user row with its refer code, its
// identities and its device. Each identity is inserted on its own, so one
// that another account holds fails the transaction instead of being dropped.
func Create(ctx context.Context, tx repository.IdentityStore, a New) error {
	u := a.User
	if err := tx.User().Insert(ctx, u); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "create user")
	}
	if u.ReferCode == "" {
		u.ReferCode = user.GenerateInviteCode(u.Id)
		if err := tx.User().UpdateColumns(ctx, u.Id, map[string]any{"refer_code": u.ReferCode}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "set refer code of user %d", u.Id)
		}
	}
	for i := range a.Identities {
		identity := a.Identities[i]
		identity.UserId = u.Id
		if err := tx.UserAuth().InsertUserAuthMethods(ctx, &identity); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "bind %s identity to user %d", identity.AuthType, u.Id)
		}
		a.Identities[i] = identity
	}
	if a.Device != nil {
		a.Device.UserId = u.Id
		if err := tx.UserDevice().InsertDevice(ctx, a.Device); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "create device of user %d", u.Id)
		}
	}
	return nil
}

// Register creates the account as a self-service registration through
// method: one identity transaction also appends the RegisteredTopic event
// and the registration audit. On an error nothing was created, and a.User
// must not be used.
func Register(ctx context.Context, store repository.IdentityTransactor, a New, method string) error {
	return store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		if err := Create(ctx, tx, a); err != nil {
			return err
		}
		if err := tx.Outbox().Append(ctx, RegisteredTopic, strconv.FormatInt(a.User.Id, 10), "{}"); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "append registration event of user %d", a.User.Id)
		}
		meta, _ := requestmeta.From(ctx)
		content, err := (&log.Register{
			AuthMethod: method,
			Identifier: logger.RedactedValue,
			RegisterIP: meta.ClientIP,
			UserAgent:  meta.UserAgent,
			Timestamp:  timeutil.Now().UnixMilli(),
		}).Marshal()
		if err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "marshal registration audit")
		}
		if err := tx.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeRegister.Uint8(),
			ObjectID: a.User.Id,
			Date:     timeutil.Now().Format(time.DateOnly),
			Content:  string(content),
		}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "record registration audit of user %d", a.User.Id)
		}
		return nil
	})
}

// EnsureActive refuses an account that can no longer sign in: a deleted one
// as not existing, a disabled one (or one without an enable flag) as
// disabled.
func EnsureActive(u *user.User) error {
	if u.DeletedAt.Valid {
		return xerr.Errorf(xerr.UserNotExist, "user %d is deleted", u.Id)
	}
	if u.Enable == nil || !*u.Enable {
		return xerr.Errorf(xerr.UserDisabled, "user %d is disabled", u.Id)
	}
	return nil
}

// Attempt is a sign-in attempt through one method. Once the flow knows the
// account it calls Identify, and it defers Finish, which audits the outcome.
type Attempt struct {
	logs   repository.LogRepo
	method string
	userID int64
}

// NewAttempt starts auditing a sign-in through method.
func NewAttempt(logs repository.LogRepo, method string) *Attempt {
	return &Attempt{logs: logs, method: method}
}

// Identify records the account the attempt signs in to. An account that
// does not exist, such as one whose registration rolled back, must not be
// identified.
func (a *Attempt) Identify(userID int64) { a.userID = userID }

// Finish audits the attempt's outcome, err, and returns the error the flow
// reports. An attempt that never identified an account leaves no audit. The
// audit belongs to a successful sign-in: when it cannot be written the
// sign-in fails. For a failed attempt a failed audit is only logged.
func (a *Attempt) Finish(ctx context.Context, err error) error {
	if a.userID == 0 {
		return err
	}
	auditErr := RecordLogin(ctx, a.logs, a.userID, a.method, err == nil)
	if auditErr == nil {
		return err
	}
	if err != nil {
		logger.WithContext(ctx).Errorw("record failed sign-in audit",
			logger.Field("user_id", a.userID), logger.Field("method", a.method), logger.Field("error", auditErr.Error()))
		return err
	}
	return auditErr
}

// RecordLogin audits a sign-in to the account userID through method. The
// client address and user agent come from the request metadata.
func RecordLogin(ctx context.Context, logs repository.LogRepo, userID int64, method string, success bool) error {
	meta, _ := requestmeta.From(ctx)
	content, err := (&log.Login{
		Method:    method,
		LoginIP:   meta.ClientIP,
		UserAgent: meta.UserAgent,
		Success:   success,
		Timestamp: timeutil.Now().UnixMilli(),
	}).Marshal()
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "marshal login audit")
	}
	if err := logs.Insert(ctx, &log.SystemLog{
		Type:     log.TypeLogin.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: userID,
		Content:  string(content),
	}); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "record login audit of user %d", userID)
	}
	return nil
}

// SessionConfig is the signing secret and lifetime, in seconds, of the
// sessions sign-ins issue.
type SessionConfig struct {
	Secret   string
	Lifetime int64
}

// Login is the successful sign-in a session is issued for.
type Login struct {
	UserID int64
	// LoginType records how the user signed in; the login type the device
	// transport put in ctx wins over it.
	LoginType string
	// Epoch is the account's session epoch the flow read (ReadEpoch) before
	// it checked the credential, or the one its own revocation set
	// (usersession.Rotate). No session is issued once the epoch moved: a
	// revocation overtook the sign-in, and the session must not outlive it.
	// Empty skips the comparison.
	Epoch string
	// Device binds the session to the device, which must be the user's and
	// enabled.
	Device *user.Device
}

// ReadEpoch returns the account's current session epoch. A sign-in reads it
// before it checks the credential and hands it on in Login.Epoch.
func ReadEpoch(ctx context.Context, store usersession.Store, userID int64) (string, error) {
	epoch, err := usersession.AcquireEpoch(ctx, store, userID)
	if err != nil {
		return "", xerr.Wrapf(err, xerr.ERROR, "read session epoch of user %d", userID)
	}
	return epoch, nil
}

// IssueSession issues the session a successful sign-in ends with. A device
// session must be bound to the device.
func IssueSession(ctx context.Context, store usersession.Store, cfg SessionConfig, login Login) (string, error) {
	loginType := login.LoginType
	if value, ok := ctx.Value(requestctx.LoginType).(string); ok {
		loginType = value
	}
	if loginType == "device" && login.Device == nil {
		return "", xerr.Errorf(xerr.InvalidAccess, "device session requires a binding")
	}
	grant := usersession.Grant{UserID: login.UserID, LoginType: loginType, Epoch: login.Epoch}
	if device := login.Device; device != nil {
		if device.Id <= 0 || device.UserId != login.UserID || !device.Enabled {
			return "", xerr.Errorf(xerr.InvalidAccess, "device session binding invalid")
		}
		grant.DeviceID = device.Id
	}
	token, err := usersession.Issue(ctx, store, cfg.Secret, cfg.Lifetime, grant)
	if errors.Is(err, usersession.ErrEpochMoved) {
		return "", xerr.Wrapf(err, xerr.InvalidAccess, "sessions of user %d were revoked during sign-in", login.UserID)
	}
	if err != nil {
		return "", xerr.Wrapf(err, xerr.ERROR, "issue session for user %d", login.UserID)
	}
	return token, nil
}
