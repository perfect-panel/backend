package repository

import (
	"context"
	"time"

	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

// UserRepo provides user profile, account, reporting, and marketing queries.
// Related authentication, subscription, device, cache, withdrawal, and traffic
// operations live behind their own focused repository interfaces below.
type UserRepo interface {
	Insert(ctx context.Context, data *user.User) error
	FindOne(ctx context.Context, id int64) (*user.User, error)
	FindAccountState(ctx context.Context, id int64) (*user.AccountState, error)
	FindOneForUpdate(ctx context.Context, id int64) (*user.User, error)
	FindOneByEmail(ctx context.Context, email string) (*user.User, error)
	FindOneByReferCode(ctx context.Context, referCode string) (*user.User, error)
	// UpdateColumns writes only the named columns. There is deliberately no
	// whole-row update: a snapshot saved back whole reverts whatever changed
	// meanwhile, including an administrator's disable or demotion.
	UpdateColumns(ctx context.Context, id int64, columns map[string]any) error
	UpgradePasswordHash(ctx context.Context, id int64, currentHash, password, algo, salt string) (bool, error)
	Delete(ctx context.Context, id int64) error
	BatchDeleteUser(ctx context.Context, ids []int64) error
	QueryPageList(ctx context.Context, page, size int, filter *user.UserFilterParams) ([]*user.User, int64, error)
	FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error)
	FindEnabledUserIDs(ctx context.Context, ids []int64) ([]int64, error)
	CountAffiliates(ctx context.Context, refererId int64) (int64, error)
	QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*user.User, int64, error)
	QueryAdminUsers(ctx context.Context) ([]*user.User, error)
	CountEnabledUsers(ctx context.Context) (int64, error)
	// QueryRegisterUserTotal counts the registered accounts; the ByDate and
	// ByMonthly variants count those registered on date's day or month.
	QueryRegisterUserTotal(ctx context.Context) (int64, error)
	QueryRegisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error)
	QueryRegisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error)
	// Deprecated: use QueryRegisterUserTotal.
	QueryResisterUserTotal(ctx context.Context) (int64, error)
	// Deprecated: use QueryRegisterUserTotalByDate.
	QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error)
	// Deprecated: use QueryRegisterUserTotalByMonthly.
	QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error)
	QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error)
	CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error)
	QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error)
	QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error)
}

// UserAuthRepo manages external authentication identities linked to users.
type UserAuthRepo interface {
	FindUserAuthMethods(ctx context.Context, userId int64) ([]*user.AuthMethods, error)
	FindUserAuthMethodsByUserIds(ctx context.Context, method string, userIds []int64) ([]*user.AuthMethods, error)
	FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error)
	// FindEmailAlias returns an email binding of a live account that reaches
	// the same mailbox as email under another spelling.
	FindEmailAlias(ctx context.Context, email string) (*user.AuthMethods, error)
	ValidateEmailIdentityUniqueness(ctx context.Context) error
	FindUserAuthMethodByPlatform(ctx context.Context, userId int64, platform string) (*user.AuthMethods, error)
	FindUserAuthMethodByUserId(ctx context.Context, method string, userId int64) (*user.AuthMethods, error)
	InsertUserAuthMethods(ctx context.Context, data *user.AuthMethods) error
	UpdateUserAuthMethods(ctx context.Context, data *user.AuthMethods) error
	DeleteUserAuthMethods(ctx context.Context, userId int64, platform string) error
	UpdateUserAuthMethodOwner(ctx context.Context, authType, identifier string, userId int64) error
	DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, identifier string) error
	UpsertUserAuthMethod(ctx context.Context, data *user.AuthMethods) error
	// NormalizeMobileIdentifiers converts the phone numbers stored in
	// another form than E.164 (the admin panel used to store
	// "<area>-<number>") to E.164. It is idempotent; a number whose E.164
	// form another binding already holds stays as it is and is reported.
	NormalizeMobileIdentifiers(ctx context.Context) (MobileNormalization, error)
}

// MobileNormalization reports what NormalizeMobileIdentifiers did.
type MobileNormalization struct {
	// Converted bindings now hold their number in E.164.
	Converted int
	// Conflicts kept their stored form because another binding already
	// holds the E.164 number; Unparsable ones are not phone numbers.
	Conflicts  int
	Unparsable int
}

// UserSubscriptionRepo manages user subscription records and their lifecycle.
type UserSubscriptionRepo interface {
	// LockUserSerial serializes subscription-creating flows per user inside
	// the current transaction (seed-and-lock on the subscription domain's
	// serial table). It replaces the fulfillment stage's cross-domain
	// user-row lock.
	LockUserSerial(ctx context.Context, userID int64) error
	InsertSubscribe(ctx context.Context, data *usersub.Subscribe) error
	FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error)
	FindOneSubscribeForUpdate(ctx context.Context, id int64) (*usersub.Subscribe, error)
	FindOneSubscribeByOrderId(ctx context.Context, orderId int64) (*usersub.Subscribe, error)
	FindOneSubscribeByToken(ctx context.Context, token string) (*usersub.Subscribe, error)
	FindOneSubscribeByTokenForUpdate(ctx context.Context, token string) (*usersub.Subscribe, error)
	// UpdateSubscribeColumns writes only the named columns of data.Id (plus
	// updated_at); concurrent writes to every other column survive. Callers
	// that derive a column from the row's current value read the row with
	// FindOneSubscribeForUpdate in the same subscription transaction first.
	// A provider-managed row accepts only usersub.LocalControlColumns and is
	// reactivated only inside its provider period; anything else fails with
	// usersub.ErrProviderManaged.
	UpdateSubscribeColumns(ctx context.Context, data *usersub.Subscribe, columns ...string) error
	// RotateSubscribeCredentials gives each subscription the token and UUID
	// of its rotation in batched column updates, and invalidates the cache
	// entries of the previous credentials as well as the new ones.
	RotateSubscribeCredentials(ctx context.Context, rotations []SubscriptionCredentialRotation) error
	// ApplyEntitlementProjection is only called with a locked provider row.
	ApplyEntitlementProjection(ctx context.Context, data *usersub.Subscribe) error
	DeleteSubscribeById(ctx context.Context, id int64) error
	// ClearSubscribeCache drops the cached entries of the subscriptions: by
	// id, by token and their owners' subscription lists.
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
	BatchUpdateUserSubscribeWithTraffic(ctx context.Context, deltas []trafficEntity.SubscribeTrafficDelta) error
	// FindUsersSubscribeBySubscribeIds returns the plans' subscriptions a
	// node may serve now (usersub.ServableCondition).
	FindUsersSubscribeBySubscribeIds(ctx context.Context, subscribeIds []int64) ([]*usersub.Subscribe, error)
	FindUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*usersub.Subscribe, error)
	FindSubscribesByIds(ctx context.Context, ids []int64) ([]*usersub.Subscribe, error)
	// FindSubscribeDetailsByIds loads the subscriptions with their plans in
	// one round trip per table; missing ids are absent from the result.
	FindSubscribeDetailsByIds(ctx context.Context, ids []int64) ([]*usersub.SubscribeDetails, error)
	// FindSubscribeDetailsByUserIds loads every subscription of the users,
	// whatever its status, with its plan: one round trip per table however
	// many users there are.
	FindSubscribeDetailsByUserIds(ctx context.Context, userIds []int64) ([]*usersub.SubscribeDetails, error)
	CountQuotaConsumingSubscriptions(ctx context.Context, userId, subscribeId int64) (int64, error)
	HasBlockingSubscription(ctx context.Context, userId int64) (bool, error)
	CountUserSubscribesBySubscribeIdAndStatus(ctx context.Context, subscribeId int64, status ...int64) (int64, error)
	QueryActiveSubscriptions(ctx context.Context, subscribeId ...int64) (map[int64]int64, error)
	QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*usersub.SubscribeDetails, error)
	FindOneSubscribeDetailsById(ctx context.Context, id int64) (*usersub.SubscribeDetails, error)
	FindOneUserSubscribe(ctx context.Context, id int64) (*usersub.SubscribeDetails, error)
	FindTrafficExceededSubscribes(ctx context.Context) ([]*usersub.Subscribe, error)
	FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*usersub.Subscribe, error)
	// FindExpiringSubscribes returns active subscriptions expiring inside the
	// window, for the pre-expiry reminder.
	FindExpiringSubscribes(ctx context.Context, from, to time.Time) ([]*usersub.Subscribe, error)
	MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time) error
	QuerySubscribeIdsByFilter(ctx context.Context, filter *usersub.SubscribeFilter) ([]int64, error)
	CountSubscribesByFilter(ctx context.Context, filter *usersub.SubscribeFilter) (int64, error)
}

// SubscriptionCredentialRotation replaces the token and UUID of one
// subscription. Previous is the row before the rotation; its cache keys are
// invalidated with the new ones.
type SubscriptionCredentialRotation struct {
	Previous *usersub.Subscribe
	Token    string
	UUID     string
}

// UserDeviceRepo manages registered devices and their online records.
type UserDeviceRepo interface {
	InsertDevice(ctx context.Context, data *user.Device) error
	FindOneDevice(ctx context.Context, id int64) (*user.Device, error)
	// FindDeviceForAuth always reads current state, bypassing cached projections.
	FindDeviceForAuth(ctx context.Context, id int64) (*user.Device, error)
	// TouchDevice only updates access metadata for the same enabled owner.
	TouchDevice(ctx context.Context, id, userID int64, ip, userAgent string) (bool, error)
	FindOneDeviceByIdentifier(ctx context.Context, id string) (*user.Device, error)
	SetDeviceEnabled(ctx context.Context, id int64, enabled bool) error
	SetDeviceOnline(ctx context.Context, id int64, online bool) error
	DeleteDevice(ctx context.Context, id int64) error
	QueryDeviceList(ctx context.Context, userid int64) ([]*user.Device, int64, error)
	QueryDevicePageList(ctx context.Context, userid, subscribeId int64, page, size int) ([]*user.Device, int64, error)
	FindDeviceOnlineRecord(ctx context.Context, userId int64, startTime, endTime string) (*user.DeviceOnlineRecord, error)
	InsertDeviceOnlineRecord(ctx context.Context, data *user.DeviceOnlineRecord) error
}

// UserWithdrawalRepo manages affiliate withdrawal records.
type UserWithdrawalRepo interface {
	InsertWithdrawal(ctx context.Context, data *walletEntity.Withdrawal) error
	FindWithdrawalForUpdate(ctx context.Context, id int64) (*walletEntity.Withdrawal, error)
	QueryWithdrawalList(ctx context.Context, userID int64, status *uint8, page, size int) ([]*walletEntity.Withdrawal, int64, error)
	UpdateWithdrawalStatus(ctx context.Context, id int64, from, to uint8, reason string) (bool, error)
}

// SubscriptionTrafficRepo manages the calendar traffic resets. A reset marks
// each subscription with the day it ran for, so a retried run never clears
// one cycle twice.
type SubscriptionTrafficRepo interface {
	// FindTrafficResetCandidates returns the in-term subscriptions of the
	// plans a calendar reset may clear at now (started, unexpired, Active or
	// Finished) that are not yet reset for day. Which of them are due is the
	// reset cycle's decision; the rows carry what it needs.
	FindTrafficResetCandidates(ctx context.Context, planIDs []int64, now, day time.Time) ([]*usersub.Subscribe, error)
	// ResetSubscribeTrafficOnce locks the given subscriptions that are still
	// candidates, clears their traffic, reactivates the Finished ones, marks
	// them reset for day and returns them. Run it inside a subscription
	// transaction together with the reset's audit rows.
	ResetSubscribeTrafficOnce(ctx context.Context, ids []int64, now, day time.Time) ([]*usersub.Subscribe, error)
}

// UserCacheRepo manages cached user-related projections.
type UserCacheRepo interface {
	ClearUserCache(ctx context.Context, data ...*user.User) error
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
	ClearDeviceCache(ctx context.Context, data ...*user.Device) error
	ClearAuthMethodCache(ctx context.Context, data ...*user.AuthMethods) error
	BatchClearRelatedCache(ctx context.Context, data *user.User) error
	// Deprecated: UpdateUserCache only ever cleared the cache; use
	// ClearUserCache.
	UpdateUserCache(ctx context.Context, data *user.User) error
	UpdateUserSubscribeCache(ctx context.Context, data *usersub.Subscribe) error
}

// The identity-family contracts. Their implementations live in the owning
// modules (identity, subscription, billing) and reach the store through the
// per-module builders in builders.go.
