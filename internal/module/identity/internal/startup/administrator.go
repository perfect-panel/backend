package startup

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateInitialAdministrator creates the first administrator, signing in
// with the verified email address (stored in its canonical form, the one
// sign-in looks up) and the password plain, when the database holds no
// account yet, and reports whether it created one. It is idempotent and safe
// to run on every start: once any account exists, the seeded administrator
// among them, nothing is created, so a configured email another account
// holds cannot fail a later start either. The count and the account share
// one identity transaction, so the account and its email binding are
// created together or not at all, and two starts cannot both seed. The refer
// code is set up front, as the bootstrap always did, so the account is
// written without a follow-up update.
func (s *Service) CreateInitialAdministrator(ctx context.Context, email, plain string) (bool, error) {
	canonicalEmail, err := identifier.ValidateEmail(email, "", false)
	if err != nil {
		return false, xerr.Wrapf(err, xerr.InvalidParams, "the administrator email is not valid")
	}
	created := false
	err = s.deps.Store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		count, err := tx.User().QueryRegisterUserTotal(ctx)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "count the accounts")
		}
		if count > 0 {
			return nil
		}
		isAdmin := true
		if err := account.Create(ctx, tx, account.New{
			User: &user.User{
				Password:  password.EncodePassWord(plain),
				Algo:      password.PasswordAlgoArgon2id,
				IsAdmin:   &isAdmin,
				ReferCode: user.GenerateInviteCode(timeutil.Now().Unix()),
			},
			Identities: []user.AuthMethods{{AuthType: identifier.Email, AuthIdentifier: canonicalEmail, Verified: true}},
		}); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

// FindAdministratorsWithPassword returns the administrators, with their auth
// methods, who sign in with the password plain: the startup warns about those
// still using the password older releases seeded, which anyone can look up.
func (s *Service) FindAdministratorsWithPassword(ctx context.Context, plain string) ([]*user.User, error) {
	admins, err := s.deps.Users.QueryAdminUsers(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query the administrators")
	}
	var matching []*user.User
	for _, admin := range admins {
		if password.MultiPasswordVerify(admin.Algo, admin.Salt, plain, admin.Password) {
			matching = append(matching, admin)
		}
	}
	return matching, nil
}
