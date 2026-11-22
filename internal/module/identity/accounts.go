package identity

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
)

// Accounts is the part of the facade other modules and the entry points use
// instead of the identity repositories (ADR-001 rules 2 and 4): entity-typed
// account reads they assemble in memory, account-cache invalidation after
// they changed data an account shows, and the account writes they ask
// identity to make. The reads return the repositories' results and errors as
// they are, so a miss is still gorm.ErrRecordNotFound.
type Accounts interface {
	// FindUser returns the account with its devices and auth methods,
	// soft-deleted ones included.
	FindUser(ctx context.Context, id int64) (*user.User, error)
	// FindDeviceForAuth returns the device's owner, identifier and enabled
	// flag as stored now, bypassing the caches: request authentication reads
	// it, so a disabled or reassigned device loses its sessions at once.
	FindDeviceForAuth(ctx context.Context, id int64) (*user.Device, error)
	// FindUsersByIDs returns the live accounts among ids, without their
	// devices and auth methods.
	FindUsersByIDs(ctx context.Context, ids []int64) ([]*user.User, error)
	// FindAccountState returns the account gate (enabled, deleted) of the
	// request hot paths, soft-deleted accounts included.
	FindAccountState(ctx context.Context, id int64) (*user.AccountState, error)
	// FindAccountStateForAuth returns the account gate request
	// authentication applies (enabled, deleted, administrator) as stored
	// now, bypassing the caches, so a ban, deletion or demotion refuses the
	// account's sessions at once rather than after the cache's lifetime.
	FindAccountStateForAuth(ctx context.Context, id int64) (*user.AccountState, error)
	// FindEnabledUserIDs returns the ids among ids of live, enabled accounts.
	FindEnabledUserIDs(ctx context.Context, ids []int64) ([]int64, error)
	// CountEnabledUsers counts the live, enabled accounts.
	CountEnabledUsers(ctx context.Context) (int64, error)

	// CountRegisteredUsers counts the registered accounts; the On and InMonth
	// variants count those registered on day's day or in month's month.
	CountRegisteredUsers(ctx context.Context) (int64, error)
	CountRegisteredUsersOn(ctx context.Context, day time.Time) (int64, error)
	CountRegisteredUsersInMonth(ctx context.Context, month time.Time) (int64, error)
	// DailyUserStatistics counts the registrations and paying users per day,
	// from the first day of until's month to until; MonthlyUserStatistics
	// per month, from five months before date on.
	DailyUserStatistics(ctx context.Context, until time.Time) ([]user.UserStatisticsWithDate, error)
	MonthlyUserStatistics(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error)

	// FindAuthMethodByIdentifier resolves the binding of authType whose
	// identifier is identifier, such as a Telegram chat id or an email
	// address.
	FindAuthMethodByIdentifier(ctx context.Context, authType, identifier string) (*user.AuthMethods, error)
	// FindEmailAlias returns an email binding of a live account that reaches
	// the same mailbox as email under another spelling (Gmail dots, "+tag"
	// subaddresses), the check registration runs; a guest purchase must
	// not create a second account for that mailbox.
	FindEmailAlias(ctx context.Context, email string) (*user.AuthMethods, error)
	// FindUserAuthMethod returns the account's binding of authType.
	FindUserAuthMethod(ctx context.Context, userID int64, authType string) (*user.AuthMethods, error)
	// ListUserAuthMethods returns every binding of the account.
	ListUserAuthMethods(ctx context.Context, userID int64) ([]*user.AuthMethods, error)
	// FindAuthMethodsByUserIDs returns the authType bindings of the accounts,
	// for contacting them in bulk; soft-deleted accounts resolve to none.
	FindAuthMethodsByUserIDs(ctx context.Context, authType string, userIDs []int64) ([]*user.AuthMethods, error)
	// ListUserDevices pages the account's devices, optionally only those of
	// one subscription.
	ListUserDevices(ctx context.Context, userID, subscribeID int64, page, size int) ([]*user.Device, int64, error)
	// ListLoginMethods returns the configured authentication methods;
	// FindLoginMethod returns the stored configuration of one, such as
	// "email" or "telegram".
	ListLoginMethods(ctx context.Context) ([]*auth.Auth, error)
	FindLoginMethod(ctx context.Context, method string) (*auth.Auth, error)
	// CountAffiliates counts the accounts the account referred;
	// ListAffiliates pages them.
	CountAffiliates(ctx context.Context, refererID int64) (int64, error)
	ListAffiliates(ctx context.Context, refererID int64, page, size int) ([]*user.User, int64, error)
	// QueryEmailRecipients returns the email addresses of the accounts a
	// marketing filter selects; CountEmailRecipients counts them.
	QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error)
	CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error)

	// ClearUserCache drops the id-keyed cached projections of the accounts,
	// after another module changed data they show.
	ClearUserCache(ctx context.Context, userIDs ...int64) error
	// ClearUserCacheOf drops every cached projection the loaded accounts
	// derive, their email-keyed entry included.
	ClearUserCacheOf(ctx context.Context, users ...*user.User) error

	// BindTelegramChat records chatID as the account's verified Telegram
	// binding, once the bot redeemed the account's bind token.
	BindTelegramChat(ctx context.Context, userID int64, chatID string) error
	// SetUserEnabled enables or disables the account like the admin edit
	// does: only the flag, then the subscription-token caches and node user
	// lists its access reaches are invalidated, so a ban applies at once.
	SetUserEnabled(ctx context.Context, userID int64, enabled bool) error
}

// accounts serves the Accounts reads and cache invalidation straight from
// the module's repositories; the writes run the subdomains' use cases on
// *service.
type accounts struct {
	users     repository.UserRepo
	userAuths repository.UserAuthRepo
	devices   repository.UserDeviceRepo
	cache     repository.UserCacheRepo
	auths     repository.AuthRepo
}

func newAccounts(deps Deps) accounts {
	return accounts{
		users:     deps.Users,
		userAuths: deps.UserAuths,
		devices:   deps.Devices,
		cache:     deps.Cache,
		auths:     deps.Auths,
	}
}

func (a accounts) FindUser(ctx context.Context, id int64) (*user.User, error) {
	return a.users.FindOne(ctx, id)
}

func (a accounts) FindDeviceForAuth(ctx context.Context, id int64) (*user.Device, error) {
	return a.devices.FindDeviceForAuth(ctx, id)
}

func (a accounts) FindUsersByIDs(ctx context.Context, ids []int64) ([]*user.User, error) {
	return a.users.FindUsersByIds(ctx, ids)
}

func (a accounts) FindAccountState(ctx context.Context, id int64) (*user.AccountState, error) {
	return a.users.FindAccountState(ctx, id)
}

// authStateReader is the uncached account-gate read the module's own
// repository implements beyond the shared repository contract.
type authStateReader interface {
	FindAccountStateForAuth(ctx context.Context, id int64) (*user.AccountState, error)
}

// The module's repository provides the uncached read.
var _ authStateReader = (*repo.UserRepo)(nil)

func (a accounts) FindAccountStateForAuth(ctx context.Context, id int64) (*user.AccountState, error) {
	if users, ok := a.users.(authStateReader); ok {
		return users.FindAccountStateForAuth(ctx, id)
	}
	// A repository without the uncached read (a test double) falls back to
	// the full account row.
	u, err := a.users.FindOne(ctx, id)
	if err != nil {
		return nil, err
	}
	return &user.AccountState{Id: u.Id, Enable: u.Enable, IsAdmin: u.IsAdmin, UpdatedAt: u.UpdatedAt, DeletedAt: u.DeletedAt}, nil
}

func (a accounts) FindEnabledUserIDs(ctx context.Context, ids []int64) ([]int64, error) {
	return a.users.FindEnabledUserIDs(ctx, ids)
}

func (a accounts) CountEnabledUsers(ctx context.Context) (int64, error) {
	return a.users.CountEnabledUsers(ctx)
}

func (a accounts) CountRegisteredUsers(ctx context.Context) (int64, error) {
	return a.users.QueryRegisterUserTotal(ctx)
}

func (a accounts) CountRegisteredUsersOn(ctx context.Context, day time.Time) (int64, error) {
	return a.users.QueryRegisterUserTotalByDate(ctx, day)
}

func (a accounts) CountRegisteredUsersInMonth(ctx context.Context, month time.Time) (int64, error) {
	return a.users.QueryRegisterUserTotalByMonthly(ctx, month)
}

func (a accounts) DailyUserStatistics(ctx context.Context, until time.Time) ([]user.UserStatisticsWithDate, error) {
	return a.users.QueryDailyUserStatisticsList(ctx, until)
}

func (a accounts) MonthlyUserStatistics(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error) {
	return a.users.QueryMonthlyUserStatisticsList(ctx, date)
}

func (a accounts) FindEmailAlias(ctx context.Context, email string) (*user.AuthMethods, error) {
	return a.userAuths.FindEmailAlias(ctx, email)
}

func (a accounts) FindAuthMethodByIdentifier(ctx context.Context, authType, identifier string) (*user.AuthMethods, error) {
	return a.userAuths.FindUserAuthMethodByOpenID(ctx, authType, identifier)
}

func (a accounts) FindUserAuthMethod(ctx context.Context, userID int64, authType string) (*user.AuthMethods, error) {
	return a.userAuths.FindUserAuthMethodByUserId(ctx, authType, userID)
}

func (a accounts) ListUserAuthMethods(ctx context.Context, userID int64) ([]*user.AuthMethods, error) {
	return a.userAuths.FindUserAuthMethods(ctx, userID)
}

func (a accounts) FindAuthMethodsByUserIDs(ctx context.Context, authType string, userIDs []int64) ([]*user.AuthMethods, error) {
	return a.userAuths.FindUserAuthMethodsByUserIds(ctx, authType, userIDs)
}

func (a accounts) ListUserDevices(ctx context.Context, userID, subscribeID int64, page, size int) ([]*user.Device, int64, error) {
	return a.devices.QueryDevicePageList(ctx, userID, subscribeID, page, size)
}

func (a accounts) ListLoginMethods(ctx context.Context) ([]*auth.Auth, error) {
	return a.auths.FindAll(ctx)
}

func (a accounts) FindLoginMethod(ctx context.Context, method string) (*auth.Auth, error) {
	return a.auths.FindOneByMethod(ctx, method)
}

func (a accounts) CountAffiliates(ctx context.Context, refererID int64) (int64, error) {
	return a.users.CountAffiliates(ctx, refererID)
}

func (a accounts) ListAffiliates(ctx context.Context, refererID int64, page, size int) ([]*user.User, int64, error) {
	return a.users.QueryAffiliateList(ctx, refererID, page, size)
}

func (a accounts) QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error) {
	return a.users.QueryEmailRecipients(ctx, filter)
}

func (a accounts) CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error) {
	return a.users.CountEmailRecipients(ctx, filter)
}

func (a accounts) ClearUserCache(ctx context.Context, userIDs ...int64) error {
	users := make([]*user.User, 0, len(userIDs))
	for _, id := range userIDs {
		users = append(users, &user.User{Id: id})
	}
	return a.cache.ClearUserCache(ctx, users...)
}

func (a accounts) ClearUserCacheOf(ctx context.Context, users ...*user.User) error {
	return a.cache.ClearUserCache(ctx, users...)
}

func (s *service) BindTelegramChat(ctx context.Context, userID int64, chatID string) error {
	return s.profile.BindTelegramChat(ctx, userID, chatID)
}

func (s *service) SetUserEnabled(ctx context.Context, userID int64, enabled bool) error {
	return s.adminUsers.SetUserEnabled(ctx, userID, enabled)
}
