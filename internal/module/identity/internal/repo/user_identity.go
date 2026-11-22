package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Identity-domain methods of the user repository: account rows, auth
// methods, devices, affiliate relations and admin page queries
// (ADR-001 step 5).

// --- user CRUD ---

func (m *UserRepo) FindOneByEmail(ctx context.Context, email string) (*user.User, error) {
	var u user.User
	canonicalEmail, err := lookupIdentifier(identifier.Email, email)
	if err != nil {
		return &u, err
	}
	key := fmt.Sprintf("%s%v", cacheUserEmailPrefix, canonicalEmail)
	err = m.QueryCtx(ctx, &u, key, func(conn *gorm.DB, v any) error {
		data, err := findUserAuthMethodByIdentifier(conn, identifier.Email, canonicalEmail)
		if err != nil {
			return err
		}
		return conn.Model(&user.User{}).Unscoped().Where("id = ?", data.UserId).Preload("UserDevices").Preload("AuthMethods").First(v).Error
	})
	return &u, err
}

func (m *UserRepo) Insert(ctx context.Context, data *user.User) error {
	for index := range data.AuthMethods {
		canonical, err := storedIdentifier(data.AuthMethods[index].AuthType, data.AuthMethods[index].AuthIdentifier)
		if err != nil {
			return err
		}
		data.AuthMethods[index].AuthIdentifier = canonical
	}
	err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		for index := range data.AuthMethods {
			if err := guardEmailIdentityWrite(conn, &data.AuthMethods[index]); err != nil {
				return err
			}
		}
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *UserRepo) FindOne(ctx context.Context, id int64) (*user.User, error) {
	userIdKey := fmt.Sprintf("%s%v", cacheUserIdPrefix, id)
	var resp user.User
	err := m.QueryCtx(ctx, &resp, userIdKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Unscoped().Where("id = ?", id).Preload("UserDevices").Preload("AuthMethods").First(&resp).Error
	})
	return &resp, err
}

func (m *UserRepo) FindAccountState(ctx context.Context, id int64) (*user.AccountState, error) {
	key := fmt.Sprintf("%s%d", cacheUserStatePrefix, id)
	var state user.AccountState
	err := m.QueryCtx(ctx, &state, key, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Unscoped().
			Select("id", "enable", "updated_at", "deleted_at").Where("id = ?", id).First(v).Error
	})
	return &state, err
}

// FindAccountStateForAuth reads the account gate request authentication
// applies (enabled, deleted, administrator) from the database, never from the
// cache: the cached account row lives for days and its invalidation is best
// effort, so a session gate reading it could serve a banned, deleted or
// demoted account long after the change. One primary-key read of four
// columns per authenticated request is the price.
func (m *UserRepo) FindAccountStateForAuth(ctx context.Context, id int64) (*user.AccountState, error) {
	var state user.AccountState
	err := m.QueryNoCacheCtx(ctx, &state, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Unscoped().
			Select("id", "enable", "is_admin", "updated_at", "deleted_at").Where("id = ?", id).First(v).Error
	})
	return &state, err
}

// FindEnabledUserIDs is the batch account-state gate used by node hot paths.
// GORM's default scope excludes soft-deleted users; the explicit enable
// predicate keeps disabled accounts from retaining service credentials.
func (m *UserRepo) FindEnabledUserIDs(ctx context.Context, ids []int64) ([]int64, error) {
	result := make([]int64, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).
			Where("id IN ? AND enable = ?", ids, true).
			Pluck("id", v).Error
	})
	return result, err
}

func (m *UserRepo) FindOneForUpdate(ctx context.Context, id int64) (*user.User, error) {
	var resp user.User
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Model(&user.User{}).
			Where("id = ?", id).
			Preload("UserDevices").
			Preload("AuthMethods").
			First(&resp).Error
	})
	return &resp, err
}

// UpdateColumns writes only the named columns of the user row. Callers name
// the fields they change, so a stale snapshot, such as the request's
// authenticated user, cannot write back an enable flag, admin flag, password
// or deletion that changed meanwhile, and a soft-deleted row stays deleted.
func (m *UserRepo) UpdateColumns(ctx context.Context, id int64, columns map[string]any) error {
	if len(columns) == 0 {
		return nil
	}
	old, err := m.FindOne(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// The columns carry the account gate (enable, is_admin), so a failed
	// invalidation is retried rather than dropped.
	return m.execInvalidating(ctx, func(conn *gorm.DB) error {
		return conn.Model(&user.User{}).Where("id = ?", id).Updates(columns).Error
	}, m.getCacheKeys(old)...)
}

func (m *UserRepo) UpgradePasswordHash(ctx context.Context, id int64, currentHash, password, algo, salt string) (bool, error) {
	old, err := m.FindOne(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	updated := false
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&user.User{}).
			Where("id = ? AND password = ?", id, currentHash).
			Updates(map[string]any{
				"password": password,
				"algo":     algo,
				"salt":     salt,
			})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected == 1
		return nil
	}, m.getCacheKeys(old)...)
	return updated, err
}

func (m *UserRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	// Every cached projection of the account goes, its subscriptions'
	// included; a failed invalidation is retried, so the deleted account is
	// not served from the cache meanwhile.
	keys := m.relatedCacheKeys(ctx, data)
	return m.execInvalidating(ctx, func(conn *gorm.DB) error {
		// Soft deletion of the account row alone: sign-in and subscriptions
		// read the deletion state.
		return conn.Model(&user.User{}).Where("id = ?", id).Delete(&user.User{}).Error
	}, keys...)
}

// --- user queries / page list ---

func (m *UserRepo) QueryPageList(ctx context.Context, page, size int, filter *user.UserFilterParams) ([]*user.User, int64, error) {
	var list []*user.User
	var total int64
	page, size = repository.NormalizePage(page, size)
	subIDs, subFiltered, err := m.subscriptionFilterUserIDs(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	err = m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		conn = applyUserPageFilters(conn.Model(&user.User{}), filter, subIDs, subFiltered)
		if err := conn.Count(&total).Error; err != nil {
			return err
		}
		return conn.Limit(size).Offset((page - 1) * size).Preload("UserDevices").Preload("AuthMethods").Find(&list).Error
	})
	return list, total, err
}

func applyUserPageFilters(conn *gorm.DB, filter *user.UserFilterParams, subIDs []int64, subFiltered bool) *gorm.DB {
	userIdColumn := userColumn(conn, "id")
	if filter == nil {
		return conn
	}
	if filter.UserId != nil {
		conn = conn.Where(userIdColumn+" = ?", *filter.UserId)
	}
	if filter.Search != "" {
		search := orm.LikePrefixPattern(filter.Search)
		if search != "" {
			conn = conn.Where(userSearchCondition(conn), search, search)
		}
	}
	if subFiltered {
		if len(subIDs) == 0 {
			conn = conn.Where("1 = 0")
		} else {
			conn = conn.Where(userIdColumn+" IN ?", subIDs)
		}
	}
	if filter.Order != "" {
		switch strings.ToUpper(filter.Order) {
		case "ASC", "DESC":
			conn = conn.Order(fmt.Sprintf("%s %s", userIdColumn, strings.ToUpper(filter.Order)))
		}
	}
	if filter.Unscoped {
		conn = conn.Unscoped()
	}
	return conn
}

// subscriptionFilterUserIDs resolves the admin filter's subscription
// conditions to a user-id list through the subscription bridge (ADR-001:
// identity SQL must not touch the user_subscribe table). The bool reports
// whether the filter constrains by subscription at all.
func (m *UserRepo) subscriptionFilterUserIDs(ctx context.Context, filter *user.UserFilterParams) ([]int64, bool, error) {
	if filter == nil {
		return nil, false, nil
	}
	token := strings.TrimSpace(filter.UserSubscribeToken)
	if filter.UserSubscribeId == nil && filter.SubscribeId == nil && token == "" {
		return nil, false, nil
	}
	bridgeFilter := repository.SubscriptionUserFilter{
		UserSubscribeID: filter.UserSubscribeId,
		SubscribeID:     filter.SubscribeId,
		Token:           token,
	}
	if token == "" {
		bridgeFilter.Statuses = []int64{0, 1}
	}
	ids, err := m.bridges.SubscriptionScope.SubscriptionUserIDs(ctx, bridgeFilter)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

func userSearchCondition(conn *gorm.DB) string {
	return fmt.Sprintf(
		"(%s LIKE ?%s OR EXISTS (SELECT 1 FROM %s WHERE %s = %s AND %s LIKE ?%s))",
		userColumn(conn, "refer_code"),
		orm.LikeEscapeClause(),
		authMethodsTableName(conn),
		authMethodsColumn(conn, "user_id"),
		userColumn(conn, "id"),
		authMethodsColumn(conn, "auth_identifier"),
		orm.LikeEscapeClause(),
	)
}

func userTableName(db *gorm.DB) string {
	return userQuoteTable(db, (&user.User{}).TableName())
}

func userColumn(db *gorm.DB, column string) string {
	return userQuoteColumn(db, (&user.User{}).TableName(), column)
}

func authMethodsTableName(db *gorm.DB) string {
	return userQuoteTable(db, (&user.AuthMethods{}).TableName())
}

func authMethodsColumn(db *gorm.DB, column string) string {
	return userQuoteColumn(db, (&user.AuthMethods{}).TableName(), column)
}

func userQuoteTable(db *gorm.DB, table string) string {
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Table{Name: table})
	}
	return table
}

func userQuoteColumn(db *gorm.DB, table, column string) string {
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Column{Table: table, Name: column})
	}
	return table + "." + column
}

// joinUndeletedUsers joins auth methods to their owning users and drops
// soft-deleted accounts. Delete leaves the bindings in place, and GORM's
// soft-delete scope covers only the model table, so a join has to state the
// predicate itself.
func joinUndeletedUsers(query *gorm.DB) *gorm.DB {
	return query.
		Joins(fmt.Sprintf("JOIN %s ON %s = %s", userTableName(query), userColumn(query, "id"), authMethodsColumn(query, "user_id"))).
		Where(userColumn(query, "deleted_at") + " IS NULL")
}

// --- user statistics / email recipients / batch delete ---

// emailRecipientQuery builds the identity-side recipient query (user +
// auth_methods only). Subscription-scope filtering happens app-side in
// subscriptionScopedUserIDs so no SQL crosses the domain boundary
// (ADR-001 step 5); the ID list is applied via scopeIDs/scopeExclude.
func emailRecipientQuery(conn *gorm.DB, filter *user.EmailRecipientFilter, scopeIDs []int64, scopeExclude bool) *gorm.DB {
	if filter == nil {
		filter = &user.EmailRecipientFilter{Scope: 1}
	}
	userID := userColumn(conn, "id")
	userCreatedAt := userColumn(conn, "created_at")
	authType := authMethodsColumn(conn, "auth_type")
	query := joinUndeletedUsers(conn.Model(&user.AuthMethods{})).
		Select("auth_identifier").
		Where(authType+" = ?", "email")

	if filter.RegisterStartTime != 0 {
		query = query.Where(userCreatedAt+" >= ?", time.UnixMilli(filter.RegisterStartTime))
	}
	if filter.RegisterEndTime != 0 {
		query = query.Where(userCreatedAt+" <= ?", time.UnixMilli(filter.RegisterEndTime))
	}

	if scopeIDs != nil {
		if scopeExclude {
			if len(scopeIDs) > 0 {
				query = query.Where(userID+" NOT IN ?", scopeIDs)
			}
		} else {
			if len(scopeIDs) == 0 {
				// An empty inclusion list matches nobody.
				query = query.Where("1 = 0")
			} else {
				query = query.Where(userID+" IN ?", scopeIDs)
			}
		}
	}
	return query
}

// subscriptionScopedUserIDs resolves the subscription-domain half of the
// recipient filter: which user IDs the scope includes (or, for scope 4,
// excludes). A nil list with exclude=false means the scope does not
// constrain by subscription.
func (m *UserRepo) subscriptionScopedUserIDs(ctx context.Context, scope int8) (ids []int64, exclude bool, err error) {
	var statuses []int64
	switch scope {
	case 2:
		statuses = []int64{1, 2}
	case 3:
		statuses = []int64{3}
	case 4:
		// Everyone who has any subscription row is excluded.
		statuses = nil
	default:
		return nil, false, nil
	}
	ids, err = m.bridges.SubscriptionScope.SubscriptionUserIDs(ctx, repository.SubscriptionUserFilter{Statuses: statuses})
	if err != nil {
		return nil, false, err
	}
	if ids == nil {
		ids = []int64{}
	}
	return ids, scope == 4, nil
}

func (m *UserRepo) QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error) {
	if filter != nil && filter.Scope == 5 {
		return nil, nil
	}
	scope := int8(1)
	if filter != nil {
		scope = filter.Scope
	}
	ids, exclude, err := m.subscriptionScopedUserIDs(ctx, scope)
	if err != nil {
		return nil, err
	}
	var emails []string
	err = m.QueryNoCacheCtx(ctx, &emails, func(conn *gorm.DB, v any) error {
		return emailRecipientQuery(conn, filter, ids, exclude).Pluck("auth_identifier", v).Error
	})
	return emails, err
}

func (m *UserRepo) CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error) {
	if filter != nil && filter.Scope == 5 {
		return 0, nil
	}
	scope := int8(1)
	if filter != nil {
		scope = filter.Scope
	}
	ids, exclude, err := m.subscriptionScopedUserIDs(ctx, scope)
	if err != nil {
		return 0, err
	}
	var total int64
	err = m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return emailRecipientQuery(conn, filter, ids, exclude).Count(&total).Error
	})
	return total, err
}

func (m *UserRepo) BatchDeleteUser(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	var users []*user.User
	err := m.QueryNoCacheCtx(ctx, &users, func(conn *gorm.DB, v any) error {
		// The auth methods carry the email lookup's cache key.
		return conn.Where("id in ?", ids).Preload("AuthMethods").Find(&users).Error
	})
	if err != nil {
		return err
	}
	// The deleted accounts' rows leave the cache with a retried invalidation,
	// so the deletion is not served from it meanwhile.
	return m.execInvalidating(ctx, func(conn *gorm.DB) error {
		return conn.Where("id in ?", ids).Delete(&user.User{}).Error
	}, m.batchGetCacheKeys(users...)...)
}

func (m *UserRepo) QueryRegisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error) {
	var total int64
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 0, 1)
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&total).Error
	})
	return total, err
}

func (m *UserRepo) QueryRegisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error) {
	var total int64
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 1, 0)
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&total).Error
	})
	return total, err
}

func (m *UserRepo) QueryRegisterUserTotal(ctx context.Context) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Count(&total).Error
	})
	return total, err
}

// Deprecated: use QueryRegisterUserTotal.
func (m *UserRepo) QueryResisterUserTotal(ctx context.Context) (int64, error) {
	return m.QueryRegisterUserTotal(ctx)
}

// Deprecated: use QueryRegisterUserTotalByDate.
func (m *UserRepo) QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error) {
	return m.QueryRegisterUserTotalByDate(ctx, date)
}

// Deprecated: use QueryRegisterUserTotalByMonthly.
func (m *UserRepo) QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error) {
	return m.QueryRegisterUserTotalByMonthly(ctx, date)
}

func (m *UserRepo) CountEnabledUsers(ctx context.Context) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Where("enable = ?", true).Count(&total).Error
	})
	return total, err
}

func (m *UserRepo) QueryAdminUsers(ctx context.Context) ([]*user.User, error) {
	var data []*user.User
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Preload("AuthMethods").Where("is_admin = ?", true).Find(&data).Error
	})
	return data, err
}

// Deprecated: UpdateUserCache only ever cleared the cache; use ClearUserCache.
func (m *UserRepo) UpdateUserCache(ctx context.Context, data *user.User) error {
	return m.ClearUserCache(ctx, data)
}

func (m *UserRepo) FindOneByReferCode(ctx context.Context, referCode string) (*user.User, error) {
	var data user.User
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Where("refer_code = ?", referCode).First(&data).Error
	})
	return &data, err
}

// QueryDailyUserStatisticsList Query daily user statistics list for the current month (from 1st to current date)
func (m *UserRepo) QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	registrations, err := m.registrationCountsByBucket(ctx, firstDay, &date, "day")
	if err != nil {
		return nil, err
	}
	newUsers, err := m.orderUserCountsByBucket(ctx, true, firstDay, &date, "day")
	if err != nil {
		return nil, err
	}
	renewalUsers, err := m.orderUserCountsByBucket(ctx, false, firstDay, &date, "day")
	if err != nil {
		return nil, err
	}
	return mergeUserStatistics(registrations, newUsers, renewalUsers), nil
}

// QueryMonthlyUserStatisticsList Query monthly user statistics list for the past 6 months
func (m *UserRepo) QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error) {
	sixMonthsAgo := date.AddDate(0, -5, 0)
	registrations, err := m.registrationCountsByBucket(ctx, sixMonthsAgo, nil, "month")
	if err != nil {
		return nil, err
	}
	newUsers, err := m.orderUserCountsByBucket(ctx, true, sixMonthsAgo, nil, "month")
	if err != nil {
		return nil, err
	}
	renewalUsers, err := m.orderUserCountsByBucket(ctx, false, sixMonthsAgo, nil, "month")
	if err != nil {
		return nil, err
	}
	return mergeUserStatistics(registrations, newUsers, renewalUsers), nil
}

// orderUserCountsByBucket delegates to the billing bridge: the query lives
// with the order table's owner and the user statistics merge happens in Go,
// so no SQL joins identity and billing tables (ADR-001 step 5).
func (m *UserRepo) orderUserCountsByBucket(ctx context.Context, isNew bool, since time.Time, until *time.Time, bucket string) (map[string]int64, error) {
	return m.bridges.OrderStats.OrderUserCountsByBucket(ctx, isNew, since, until, bucket)
}

// registrationCountsByBucket aggregates new registrations per date bucket
// (identity-domain only).
func (m *UserRepo) registrationCountsByBucket(ctx context.Context, since time.Time, until *time.Time, bucket string) ([]user.UserStatisticsWithDate, error) {
	var results []user.UserStatisticsWithDate
	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v any) error {
		userCreatedAt := userColumn(conn, "created_at")
		userDateExpr := orm.DateBucketExpr(conn, userCreatedAt, bucket)
		q := conn.Model(&user.User{}).
			Select(fmt.Sprintf("%s AS date, COUNT(*) AS register", userDateExpr))
		if until != nil {
			q = q.Where(userCreatedAt+" BETWEEN ? AND ?", since, *until)
		} else {
			q = q.Where(userCreatedAt+" >= ?", since)
		}
		return q.Group(userDateExpr).Order("date ASC").Scan(v).Error
	})
	return results, err
}

func mergeUserStatistics(registrations []user.UserStatisticsWithDate, newUsers, renewalUsers map[string]int64) []user.UserStatisticsWithDate {
	for i := range registrations {
		registrations[i].NewOrderUsers = newUsers[registrations[i].Date]
		registrations[i].RenewalOrderUsers = renewalUsers[registrations[i].Date]
	}
	return registrations
}

// --- auth methods ---

func (m *UserRepo) FindUserAuthMethods(ctx context.Context, userId int64) ([]*user.AuthMethods, error) {
	var data []*user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.AuthMethods{}).Where("user_id = ?", userId).Find(&data).Error
	})
	return data, err
}

// FindUserAuthMethodsByUserIds resolves the bindings used to contact users
// in bulk; soft-deleted users resolve to none.
func (m *UserRepo) FindUserAuthMethodsByUserIds(ctx context.Context, method string, userIds []int64) ([]*user.AuthMethods, error) {
	if len(userIds) == 0 {
		return []*user.AuthMethods{}, nil
	}
	var data []*user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return joinUndeletedUsers(conn.Model(&user.AuthMethods{})).
			Where(authMethodsColumn(conn, "auth_type")+" = ? AND "+authMethodsColumn(conn, "user_id")+" IN ?", method, userIds).
			Find(v).Error
	})
	return data, err
}

// FindEmailAlias returns an email binding of an account that is not deleted
// and reaches the same mailbox as email under another spelling (see
// identifier.EmailMailboxKey), or gorm.ErrRecordNotFound. The LIKE pattern
// keeps the key's characters in order with anything between them, which
// covers inserted Gmail dots and "+tag" subaddresses; its literal first
// character keeps the lookup on the (auth_type, auth_identifier) index.
func (m *UserRepo) FindEmailAlias(ctx context.Context, email string) (*user.AuthMethods, error) {
	canonical := identifier.CanonicalEmail(email)
	key := identifier.EmailMailboxKey(canonical)
	at := strings.LastIndex(key, "@")
	if at <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	local, domain := key[:at], key[at+1:]
	domains := []string{domain}
	if domain == "gmail.com" {
		domains = append(domains, "googlemail.com")
	}
	var candidates []*user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &candidates, func(conn *gorm.DB, v any) error {
		column := authMethodsColumn(conn, "auth_identifier")
		conditions := make([]string, 0, len(domains))
		args := make([]any, 0, len(domains))
		for _, d := range domains {
			conditions = append(conditions, column+" LIKE ?"+orm.LikeEscapeClause())
			args = append(args, emailAliasPattern(local, d))
		}
		return joinUndeletedUsers(conn.Model(&user.AuthMethods{})).
			Where(authMethodsColumn(conn, "auth_type")+" = ?", identifier.Email).
			Where("("+strings.Join(conditions, " OR ")+")", args...).
			Find(v).Error
	})
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if candidate.AuthIdentifier != canonical && identifier.EmailMailboxKey(candidate.AuthIdentifier) == key {
			return candidate, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// emailAliasPattern matches the addresses whose mailbox key has the given
// local part at domain: its characters in order, anything between and after.
func emailAliasPattern(local, domain string) string {
	var pattern strings.Builder
	for _, r := range local {
		pattern.WriteString(orm.LikeEscape(string(r)))
		pattern.WriteByte('%')
	}
	pattern.WriteString("@" + orm.LikeEscape(domain))
	return pattern.String()
}

func (m *UserRepo) FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error) {
	var data user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		resolved, err := findUserAuthMethodByIdentifier(conn, method, openID)
		if err != nil {
			return err
		}
		data = *resolved
		return nil
	})
	return &data, err
}

func (m *UserRepo) FindUserAuthMethodByPlatform(ctx context.Context, userId int64, platform string) (*user.AuthMethods, error) {
	var data user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", userId, platform).First(&data).Error
	})
	return &data, err
}

func (m *UserRepo) InsertUserAuthMethods(ctx context.Context, data *user.AuthMethods) error {
	canonical, err := storedIdentifier(data.AuthType, data.AuthIdentifier)
	if err != nil {
		return err
	}
	data.AuthIdentifier = canonical
	u, err := m.FindOne(ctx, data.UserId)
	if err != nil {
		return err
	}

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if err = guardEmailIdentityWrite(conn, data); err != nil {
			return err
		}
		if err = conn.Model(&user.AuthMethods{}).Create(data).Error; err != nil {
			return err
		}
		// The database write is the source of truth. Cache invalidation is queued
		// for Store.InTx and best-effort for standalone writes.
		_ = m.clearBindingCache(ctx, u, data)
		return nil
	})
}

// clearBindingCache drops the cached rows a binding write changed: the
// account's, as loaded before the write, and the written identifier's own
// lookup. The account's keys cover only the identifiers it had before the
// write, while the new identifier's lookup may hold a remembered miss from
// the duplicate check that preceded the write.
func (m *UserRepo) clearBindingCache(ctx context.Context, u *user.User, binding *user.AuthMethods) error {
	keys := append(m.getCacheKeys(u), binding.GetCacheKeys()...)
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *UserRepo) UpdateUserAuthMethods(ctx context.Context, data *user.AuthMethods) error {
	canonical, err := storedIdentifier(data.AuthType, data.AuthIdentifier)
	if err != nil {
		return err
	}
	data.AuthIdentifier = canonical
	u, err := m.FindOne(ctx, data.UserId)
	if err != nil {
		return err
	}

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if err = guardEmailIdentityWrite(conn, data); err != nil {
			return err
		}
		// Only the columns a binding update changes are written: a whole-row
		// save of a stale snapshot would revert the user id, the type or the
		// creation time to what the caller loaded.
		err = conn.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", data.UserId, data.AuthType).
			Updates(map[string]any{"auth_identifier": data.AuthIdentifier, "verified": data.Verified}).Error
		if err != nil {
			return err
		}
		// See InsertUserAuthMethods: never report a committed database update as
		// failed solely because Redis is unavailable.
		_ = m.clearBindingCache(ctx, u, data)
		return nil
	})
}

func (m *UserRepo) DeleteUserAuthMethods(ctx context.Context, userId int64, platform string) error {
	u, err := m.FindOne(ctx, userId)
	if err != nil {
		return err
	}
	defer func() {
		// The write may have committed although the request ended
		// meanwhile, so the cached rows are dropped without its
		// cancellation.
		if err = m.ClearUserCache(context.WithoutCancel(ctx), u); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", userId, platform).Delete(&user.AuthMethods{}).Error
	})
}

func (m *UserRepo) UpdateUserAuthMethodOwner(ctx context.Context, authType, authIdentifier string, userId int64) error {
	authMethod, err := m.FindUserAuthMethodByOpenID(ctx, authType, authIdentifier)
	if err != nil {
		return err
	}
	oldUser, err := m.FindOne(ctx, authMethod.UserId)
	if err != nil {
		return err
	}
	newUser, err := m.FindOne(ctx, userId)
	if err != nil {
		return err
	}
	defer func() {
		// As in DeleteUserAuthMethods, the cached rows are dropped without
		// the request's cancellation.
		if err = m.ClearUserCache(context.WithoutCancel(ctx), oldUser, newUser); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&user.AuthMethods{}).
			Where("id = ?", authMethod.Id).
			Update("user_id", userId).Error
	})
}

func (m *UserRepo) DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, authIdentifier string) error {
	authMethod, err := m.FindUserAuthMethodByOpenID(ctx, authType, authIdentifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	u, err := m.FindOne(ctx, authMethod.UserId)
	if err != nil {
		return err
	}
	defer func() {
		// As in DeleteUserAuthMethods, the cached rows are dropped without
		// the request's cancellation.
		if err = m.ClearUserCache(context.WithoutCancel(ctx), u); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&user.AuthMethods{}).
			Where("id = ?", authMethod.Id).
			Delete(&user.AuthMethods{}).Error
	})
}

func (m *UserRepo) UpsertUserAuthMethod(ctx context.Context, data *user.AuthMethods) error {
	current, err := m.FindUserAuthMethodByPlatform(ctx, data.UserId, data.AuthType)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m.InsertUserAuthMethods(ctx, data)
		}
		return err
	}
	current.AuthIdentifier = data.AuthIdentifier
	current.Verified = data.Verified
	return m.UpdateUserAuthMethods(ctx, current)
}

func (m *UserRepo) FindUserAuthMethodByUserId(ctx context.Context, method string, userId int64) (*user.AuthMethods, error) {
	var data user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.AuthMethods{}).Where("auth_type = ? AND user_id = ?", method, userId).First(&data).Error
	})
	return &data, err
}

// --- device ---

func (m *UserRepo) FindDeviceForAuth(ctx context.Context, id int64) (*user.Device, error) {
	var device user.Device
	err := m.QueryNoCacheCtx(ctx, &device, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.Device{}).Select("id", "user_id", "identifier", "enabled").Where("id = ?", id).First(v).Error
	})
	return &device, err
}

func (m *UserRepo) TouchDevice(ctx context.Context, id, userID int64, ip, userAgent string) (bool, error) {
	device, err := m.FindDeviceForAuth(ctx, id)
	if err != nil {
		return false, err
	}
	var changed bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&user.Device{}).Where("id = ? AND user_id = ? AND enabled = ?", id, userID, true).
			Updates(map[string]any{"ip": ip, "user_agent": userAgent})
		changed = result.RowsAffected == 1
		return result.Error
	}, device.GetCacheKeys()...)
	if err == nil && !changed {
		// MySQL may report zero affected rows when all metadata (including
		// a second-resolution updated_at) is unchanged. Distinguish that from
		// a deleted, disabled, or reassigned binding without trusting cache.
		current, queryErr := m.FindDeviceForAuth(ctx, id)
		if errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return false, nil
		}
		if queryErr != nil {
			return false, queryErr
		}
		changed = current.UserId == userID && current.Enabled
	}
	return changed, err
}

func (m *UserRepo) FindOneDevice(ctx context.Context, id int64) (*user.Device, error) {
	deviceIdKey := fmt.Sprintf("%s%v", cacheUserDeviceIdPrefix, id)
	var resp user.Device
	err := m.QueryCtx(ctx, &resp, deviceIdKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.Device{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *UserRepo) FindOneDeviceByIdentifier(ctx context.Context, id string) (*user.Device, error) {
	deviceIdKey := fmt.Sprintf("%s%v", cacheUserDeviceNumberPrefix, id)
	var resp user.Device
	err := m.QueryCtx(ctx, &resp, deviceIdKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.Device{}).Where("identifier = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// QueryDevicePageList  returns a list of records that meet the conditions.
func (m *UserRepo) QueryDevicePageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*user.Device, int64, error) {
	var list []*user.Device
	var total int64
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.Device{}).Where("user_id = ? and subscribe_id = ?", userId, subscribeId).Count(&total).Limit(size).Offset((page - 1) * size).Find(&list).Error
	})
	return list, total, err
}

// QueryDeviceList  returns a list of records that meet the conditions.
func (m *UserRepo) QueryDeviceList(ctx context.Context, userId int64) ([]*user.Device, int64, error) {
	var list []*user.Device
	var total int64
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.Device{}).Where("user_id = ?", userId).Count(&total).Find(&list).Error
	})
	return list, total, err
}

func (m *UserRepo) SetDeviceEnabled(ctx context.Context, id int64, enabled bool) error {
	return m.updateDeviceField(ctx, id, "enabled", enabled)
}

func (m *UserRepo) SetDeviceOnline(ctx context.Context, id int64, online bool) error {
	return m.updateDeviceField(ctx, id, "online", online)
}

func (m *UserRepo) updateDeviceField(ctx context.Context, id int64, field string, value bool) error {
	old, err := m.FindDeviceForAuth(ctx, id)
	if err != nil {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		query := conn.Model(&user.Device{}).Where("id = ?", id)
		if field == "enabled" && !value {
			return query.Updates(map[string]any{"enabled": false, "online": false}).Error
		}
		if field == "online" && value {
			query = query.Where("enabled = ?", true)
		}
		return query.Update(field, value).Error
	}, old.GetCacheKeys()...)
}

func (m *UserRepo) DeleteDevice(ctx context.Context, id int64) error {
	data, err := m.FindOneDevice(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Delete(&user.Device{}, id).Error
	}, data.GetCacheKeys()...)
	return err
}

func (m *UserRepo) InsertDevice(ctx context.Context, data *user.Device) error {
	defer func() {
		// The insert is the source of truth; a stale lookup cache only
		// delays seeing the new device.
		if clearErr := m.ClearDeviceCache(ctx, data); clearErr != nil {
			logger.WithContext(ctx).Errorw("[UserModel] clear device cache failed", logger.Field("error", clearErr.Error()))
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(data).Error
	})
}

func (m *UserRepo) FindDeviceOnlineRecord(ctx context.Context, userId int64, startTime, endTime string) (*user.DeviceOnlineRecord, error) {
	var record user.DeviceOnlineRecord
	err := m.QueryNoCacheCtx(ctx, &record, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.DeviceOnlineRecord{}).
			Where("user_id = ? AND created_at >= ? AND created_at < ?", userId, startTime, endTime).
			First(&record).Error
	})
	return &record, err
}

func (m *UserRepo) InsertDeviceOnlineRecord(ctx context.Context, data *user.DeviceOnlineRecord) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(data).Error
	})
}

// --- affiliate / batch / multi-id queries ---

func (m *UserRepo) CountAffiliates(ctx context.Context, refererId int64) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Where("referer_id = ?", refererId).Count(&total).Error
	})
	return total, err
}

func (m *UserRepo) QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*user.User, int64, error) {
	var list []*user.User
	var total int64
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).
			Where("referer_id = ?", refererId).
			Count(&total).
			Order("id desc").
			Limit(size).
			Offset((page - 1) * size).
			Preload("AuthMethods").
			Find(&list).Error
	})
	return list, total, err
}

func (m *UserRepo) FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error) {
	var users []*user.User
	if len(ids) == 0 {
		return users, nil
	}
	err := m.QueryNoCacheCtx(ctx, &users, func(conn *gorm.DB, v any) error {
		return conn.Model(&user.User{}).Where("id IN ?", ids).Find(&users).Error
	})
	return users, err
}
