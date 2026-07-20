package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/order"
	trafficEntity "github.com/perfect-panel/server/internal/model/entity/traffic"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	cacheUserIdPrefix             = "cache:user:id:"
	cacheUserEmailPrefix          = "cache:user:email:v2:"
	cacheUserSubscribeTokenPrefix = "cache:user:subscribe:token:"
	cacheUserSubscribeUserPrefix  = "cache:user:subscribe:user:"
	cacheUserSubscribeIdPrefix    = "cache:user:subscribe:id:"
	cacheUserDeviceNumberPrefix   = "cache:user:device:number:"
	cacheUserDeviceIdPrefix       = "cache:user:device:id:"
)

// UserRepo user 数据访问接口
type UserRepo interface {
	// user
	Insert(ctx context.Context, data *user.User, tx ...*gorm.DB) error
	FindOne(ctx context.Context, id int64) (*user.User, error)
	FindOneForUpdate(ctx context.Context, id int64) (*user.User, error)
	FindOneByEmail(ctx context.Context, email string) (*user.User, error)
	FindOneByReferCode(ctx context.Context, referCode string) (*user.User, error)
	Update(ctx context.Context, data *user.User, tx ...*gorm.DB) error
	UpdateBalanceFields(ctx context.Context, data *user.User, tx ...*gorm.DB) error
	Delete(ctx context.Context, id int64, tx ...*gorm.DB) error
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
	BatchDeleteUser(ctx context.Context, ids []int64, tx ...*gorm.DB) error
	QueryPageList(ctx context.Context, page, size int, filter *user.UserFilterParams) ([]*user.User, int64, error)
	FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error)
	CountAffiliates(ctx context.Context, refererId int64) (int64, error)
	QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*user.User, int64, error)
	QueryAdminUsers(ctx context.Context) ([]*user.User, error)
	CountEnabledUsers(ctx context.Context) (int64, error)
	QueryResisterUserTotal(ctx context.Context) (int64, error)
	QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error)
	QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error)
	QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error)
	CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error)
	QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error)
	QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error)

	// auth methods
	FindUserAuthMethods(ctx context.Context, userId int64) ([]*user.AuthMethods, error)
	FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error)
	ValidateEmailIdentityUniqueness(ctx context.Context) error
	FindUserAuthMethodByPlatform(ctx context.Context, userId int64, platform string) (*user.AuthMethods, error)
	FindUserAuthMethodByUserId(ctx context.Context, method string, userId int64) (*user.AuthMethods, error)
	InsertUserAuthMethods(ctx context.Context, data *user.AuthMethods, tx ...*gorm.DB) error
	UpdateUserAuthMethods(ctx context.Context, data *user.AuthMethods, tx ...*gorm.DB) error
	DeleteUserAuthMethods(ctx context.Context, userId int64, platform string, tx ...*gorm.DB) error
	UpdateUserAuthMethodOwner(ctx context.Context, authType, identifier string, userId int64, tx ...*gorm.DB) error
	DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, identifier string, tx ...*gorm.DB) error
	UpsertUserAuthMethod(ctx context.Context, data *user.AuthMethods) error

	// subscribe
	InsertSubscribe(ctx context.Context, data *user.Subscribe, tx ...*gorm.DB) error
	FindOneSubscribe(ctx context.Context, id int64) (*user.Subscribe, error)
	FindOneSubscribeByOrderId(ctx context.Context, orderId int64) (*user.Subscribe, error)
	FindOneSubscribeByToken(ctx context.Context, token string) (*user.Subscribe, error)
	UpdateSubscribe(ctx context.Context, data *user.Subscribe, tx ...*gorm.DB) error
	DeleteSubscribe(ctx context.Context, token string, tx ...*gorm.DB) error
	DeleteSubscribeById(ctx context.Context, id int64, tx ...*gorm.DB) error
	UpdateUserSubscribeWithTraffic(ctx context.Context, id, download, upload int64, tx ...*gorm.DB) error
	BatchUpdateUserSubscribeWithTraffic(ctx context.Context, deltas []trafficEntity.SubscribeTrafficDelta, tx ...*gorm.DB) error
	FindUsersSubscribeBySubscribeId(ctx context.Context, subscribeId int64) ([]*user.Subscribe, error)
	FindUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*user.Subscribe, error)
	FindSubscribesByIds(ctx context.Context, ids []int64) ([]*user.Subscribe, error)
	ActivatePendingSubscribesBySubscribeId(ctx context.Context, subscribeId int64) error
	CountUserSubscribesByUserAndSubscribe(ctx context.Context, userId, subscribeId int64) (int64, error)
	CountUserSubscribesBySubscribeIdAndStatus(ctx context.Context, subscribeId int64, status ...int64) (int64, error)
	QueryActiveSubscriptions(ctx context.Context, subscribeId ...int64) (map[int64]int64, error)
	QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*user.SubscribeDetails, error)
	FindOneSubscribeDetailsById(ctx context.Context, id int64) (*user.SubscribeDetails, error)
	FindOneUserSubscribe(ctx context.Context, id int64) (*user.SubscribeDetails, error)
	FindTrafficExceededSubscribes(ctx context.Context) ([]*user.Subscribe, error)
	FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*user.Subscribe, error)
	MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time, tx ...*gorm.DB) error
	QuerySubscribeIdsByFilter(ctx context.Context, filter *user.SubscribeFilter) ([]int64, error)
	CountSubscribesByFilter(ctx context.Context, filter *user.SubscribeFilter) (int64, error)

	// device
	InsertDevice(ctx context.Context, data *user.Device, tx ...*gorm.DB) error
	FindOneDevice(ctx context.Context, id int64) (*user.Device, error)
	FindOneDeviceByIdentifier(ctx context.Context, id string) (*user.Device, error)
	UpdateDevice(ctx context.Context, data *user.Device, tx ...*gorm.DB) error
	DeleteDevice(ctx context.Context, id int64, tx ...*gorm.DB) error
	QueryDeviceList(ctx context.Context, userid int64) ([]*user.Device, int64, error)
	QueryDevicePageList(ctx context.Context, userid, subscribeId int64, page, size int) ([]*user.Device, int64, error)
	FindDeviceOnlineRecord(ctx context.Context, userId int64, startTime, endTime string) (*user.DeviceOnlineRecord, error)
	InsertDeviceOnlineRecord(ctx context.Context, data *user.DeviceOnlineRecord, tx ...*gorm.DB) error

	// withdrawal
	InsertWithdrawal(ctx context.Context, data *user.Withdrawal, tx ...*gorm.DB) error

	// reset traffic
	QueryMonthlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error)
	QueryFirstResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error)
	QueryYearlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error)
	ResetSubscribeTrafficByIds(ctx context.Context, ids []int64, tx ...*gorm.DB) error

	// cache
	ClearUserCache(ctx context.Context, data ...*user.User) error
	ClearSubscribeCache(ctx context.Context, data ...*user.Subscribe) error
	ClearDeviceCache(ctx context.Context, data ...*user.Device) error
	ClearAuthMethodCache(ctx context.Context, data ...*user.AuthMethods) error
	BatchClearRelatedCache(ctx context.Context, data *user.User) error
	UpdateUserCache(ctx context.Context, data *user.User) error
	UpdateUserSubscribeCache(ctx context.Context, data *user.Subscribe) error
}

var _ UserRepo = (*userRepo)(nil)

type userRepo struct {
	cache.CachedConn
	table string
}

func newUserRepo(db *gorm.DB, c *redis.Client) UserRepo {
	return &userRepo{
		CachedConn: cache.NewConn(db, c),
		table:      "user",
	}
}

// --- internal helpers ---

func (m *userRepo) getCacheKeys(data *user.User) []string {
	if data == nil {
		return []string{}
	}
	return data.GetCacheKeys()
}

func (m *userRepo) batchGetCacheKeys(users ...*user.User) []string {
	var keys []string
	for _, u := range users {
		keys = append(keys, u.GetCacheKeys()...)
	}
	return keys
}

// --- user CRUD ---

func (m *userRepo) FindOneByEmail(ctx context.Context, email string) (*user.User, error) {
	var u user.User
	canonicalEmail, err := canonicalAuthIdentifier(authmethod.Email, email)
	if err != nil {
		return &u, err
	}
	key := fmt.Sprintf("%s%v", cacheUserEmailPrefix, canonicalEmail)
	err = m.QueryCtx(ctx, &u, key, func(conn *gorm.DB, v interface{}) error {
		data, err := findUserAuthMethodByIdentifier(conn, authmethod.Email, canonicalEmail)
		if err != nil {
			return err
		}
		return conn.Model(&user.User{}).Unscoped().Where("id = ?", data.UserId).Preload("UserDevices").Preload("AuthMethods").First(v).Error
	})
	return &u, err
}

func (m *userRepo) Insert(ctx context.Context, data *user.User, tx ...*gorm.DB) error {
	for index := range data.AuthMethods {
		identifier, err := canonicalAuthIdentifier(data.AuthMethods[index].AuthType, data.AuthMethods[index].AuthIdentifier)
		if err != nil {
			return err
		}
		data.AuthMethods[index].AuthIdentifier = identifier
	}
	err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		for index := range data.AuthMethods {
			if err := guardEmailIdentityWrite(conn, &data.AuthMethods[index]); err != nil {
				return err
			}
		}
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *userRepo) FindOne(ctx context.Context, id int64) (*user.User, error) {
	userIdKey := fmt.Sprintf("%s%v", cacheUserIdPrefix, id)
	var resp user.User
	err := m.QueryCtx(ctx, &resp, userIdKey, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Unscoped().Where("id = ?", id).Preload("UserDevices").Preload("AuthMethods").First(&resp).Error
	})
	return &resp, err
}

func (m *userRepo) FindOneForUpdate(ctx context.Context, id int64) (*user.User, error) {
	var resp user.User
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Model(&user.User{}).
			Where("id = ?", id).
			Preload("UserDevices").
			Preload("AuthMethods").
			First(&resp).Error
	})
	return &resp, err
}

func (m *userRepo) Update(ctx context.Context, data *user.User, tx ...*gorm.DB) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Save(data).Error
	}, m.getCacheKeys(old)...)
	return err
}

func (m *userRepo) UpdateBalanceFields(ctx context.Context, data *user.User, tx ...*gorm.DB) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.User{}).
			Where("id = ?", data.Id).
			Updates(map[string]interface{}{
				"balance":     data.Balance,
				"gift_amount": data.GiftAmount,
			}).Error
	}, m.getCacheKeys(data)...)
}

func (m *userRepo) Delete(ctx context.Context, id int64, tx ...*gorm.DB) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	// Use batch related cache cleaning, including a cache of all relevant data
	defer func() {
		if clearErr := m.BatchClearRelatedCache(ctx, data); clearErr != nil {
			// Record cache cleaning errors, but do not block deletion operations
			logger.Errorf("failed to clear related cache for user %d: %v", id, clearErr.Error())
		}
	}()

	return m.TransactCtx(ctx, func(db *gorm.DB) error {
		if len(tx) > 0 {
			db = tx[0]
		}
		// Soft deletion of user information without any processing of other information (Determine whether to allow login/subscription based on the user's deletion status)
		if err := db.Model(&user.User{}).Where("id = ?", id).Delete(&user.User{}).Error; err != nil {
			return err
		}

		return nil
	})
}

func (m *userRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.TransactCtx(ctx, fn)
}

// --- user queries / page list ---

func (m *userRepo) QueryPageList(ctx context.Context, page, size int, filter *user.UserFilterParams) ([]*user.User, int64, error) {
	var list []*user.User
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		conn = applyUserPageFilters(conn.Model(&user.User{}), filter)
		if err := conn.Count(&total).Error; err != nil {
			return err
		}
		return conn.Limit(size).Offset((page - 1) * size).Preload("UserDevices").Preload("AuthMethods").Find(&list).Error
	})
	return list, total, err
}

func applyUserPageFilters(conn *gorm.DB, filter *user.UserFilterParams) *gorm.DB {
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
	if filter.UserSubscribeId != nil || filter.SubscribeId != nil || strings.TrimSpace(filter.UserSubscribeToken) != "" {
		conn = userSubscribeExistsCondition(conn, userIdColumn, filter)
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

func userSubscribeExistsCondition(conn *gorm.DB, userIdColumn string, filter *user.UserFilterParams) *gorm.DB {
	conditions := []string{
		fmt.Sprintf("%s = %s", userSubscribeColumn(conn, "user_id"), userIdColumn),
	}
	args := make([]interface{}, 0, 5)
	if filter.UserSubscribeId != nil {
		conditions = append(conditions, fmt.Sprintf("%s = ?", userSubscribeColumn(conn, "id")))
		args = append(args, *filter.UserSubscribeId)
	}
	if filter.SubscribeId != nil {
		conditions = append(conditions, fmt.Sprintf("%s = ?", userSubscribeColumn(conn, "subscribe_id")))
		args = append(args, *filter.SubscribeId)
	}
	subscribeToken := strings.TrimSpace(filter.UserSubscribeToken)
	if subscribeToken != "" {
		conditions = append(conditions, fmt.Sprintf("(%s = ? OR %s = ?)", userSubscribeColumn(conn, "token"), userSubscribeColumn(conn, "uuid")))
		args = append(args, subscribeToken, subscribeToken)
	} else {
		conditions = append(conditions, fmt.Sprintf("%s IN ?", userSubscribeColumn(conn, "status")))
		args = append(args, []int64{0, 1})
	}
	return conn.Where(
		fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s)",
			userSubscribeTableName(conn),
			strings.Join(conditions, " AND "),
		),
		args...,
	)
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

func userSubscribeTableName(db *gorm.DB) string {
	return userQuoteTable(db, (&user.Subscribe{}).TableName())
}

func userSubscribeColumn(db *gorm.DB, column string) string {
	return userQuoteColumn(db, (&user.Subscribe{}).TableName(), column)
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

// --- user statistics / email recipients / batch delete ---

func emailRecipientQuery(conn *gorm.DB, filter *user.EmailRecipientFilter) *gorm.DB {
	if filter == nil {
		filter = &user.EmailRecipientFilter{Scope: 1}
	}
	userID := userColumn(conn, "id")
	userCreatedAt := userColumn(conn, "created_at")
	authUserID := authMethodsColumn(conn, "user_id")
	authType := authMethodsColumn(conn, "auth_type")
	query := conn.Model(&user.AuthMethods{}).
		Select("auth_identifier").
		Joins(fmt.Sprintf("JOIN %s ON %s = %s", userTableName(conn), userID, authUserID)).
		Where(authType+" = ?", "email")

	if filter.RegisterStartTime != 0 {
		query = query.Where(userCreatedAt+" >= ?", time.UnixMilli(filter.RegisterStartTime))
	}
	if filter.RegisterEndTime != 0 {
		query = query.Where(userCreatedAt+" <= ?", time.UnixMilli(filter.RegisterEndTime))
	}

	switch filter.Scope {
	case 2:
		query = query.Joins(fmt.Sprintf("JOIN user_subscribe ON %s = user_subscribe.user_id", userID)).
			Where("user_subscribe.status IN ?", []int64{1, 2})
	case 3:
		query = query.Joins(fmt.Sprintf("JOIN user_subscribe ON %s = user_subscribe.user_id", userID)).
			Where("user_subscribe.status = ?", 3)
	case 4:
		query = query.Joins(fmt.Sprintf("LEFT JOIN user_subscribe ON %s = user_subscribe.user_id", userID)).
			Where("user_subscribe.user_id IS NULL")
	}
	return query
}

func (m *userRepo) QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error) {
	if filter != nil && filter.Scope == 5 {
		return nil, nil
	}
	var emails []string
	err := m.QueryNoCacheCtx(ctx, &emails, func(conn *gorm.DB, v interface{}) error {
		return emailRecipientQuery(conn, filter).Pluck("auth_identifier", v).Error
	})
	return emails, err
}

func (m *userRepo) CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error) {
	if filter != nil && filter.Scope == 5 {
		return 0, nil
	}
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return emailRecipientQuery(conn, filter).Count(&total).Error
	})
	return total, err
}

func subscribeFilterQuery(conn *gorm.DB, filter *user.SubscribeFilter) *gorm.DB {
	query := conn.Model(&user.Subscribe{})
	if filter == nil {
		return query
	}
	if len(filter.Subscribers) > 0 {
		query = query.Where("subscribe_id IN ?", filter.Subscribers)
	}
	if filter.IsActive != nil && *filter.IsActive {
		query = query.Where("status IN ?", []int64{0, 1, 2})
	}
	if filter.StartTime != 0 {
		query = query.Where("start_time <= ?", time.UnixMilli(filter.StartTime))
	}
	if filter.EndTime != 0 {
		query = query.Where("expire_time >= ?", time.UnixMilli(filter.EndTime))
	}
	return query
}

func (m *userRepo) QuerySubscribeIdsByFilter(ctx context.Context, filter *user.SubscribeFilter) ([]int64, error) {
	var ids []int64
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v interface{}) error {
		return subscribeFilterQuery(conn, filter).Pluck("id", v).Error
	})
	return ids, err
}

func (m *userRepo) CountSubscribesByFilter(ctx context.Context, filter *user.SubscribeFilter) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return subscribeFilterQuery(conn, filter).Count(&total).Error
	})
	return total, err
}

func (m *userRepo) BatchDeleteUser(ctx context.Context, ids []int64, tx ...*gorm.DB) error {
	if len(ids) == 0 {
		return nil
	}
	var users []*user.User
	err := m.QueryNoCacheCtx(ctx, &users, func(conn *gorm.DB, v interface{}) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Where("id in ?", ids).Find(&users).Error
	})
	if err != nil {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Where("id in ?", ids).Delete(&user.User{}).Error
	}, m.batchGetCacheKeys(users...)...)
}

func (m *userRepo) QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error) {
	var total int64
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 0, 1)
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&total).Error
	})
	return total, err
}

func (m *userRepo) QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error) {
	var total int64
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 1, 0)
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&total).Error
	})
	return total, err
}

func (m *userRepo) QueryResisterUserTotal(ctx context.Context) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Count(&total).Error
	})
	return total, err
}

func (m *userRepo) CountEnabledUsers(ctx context.Context) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Where("enable = ?", true).Count(&total).Error
	})
	return total, err
}

func (m *userRepo) QueryAdminUsers(ctx context.Context) ([]*user.User, error) {
	var data []*user.User
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Preload("AuthMethods").Where("is_admin = ?", true).Find(&data).Error
	})
	return data, err
}

func (m *userRepo) UpdateUserCache(ctx context.Context, data *user.User) error {
	return m.ClearUserCache(ctx, data)
}

func (m *userRepo) FindOneByReferCode(ctx context.Context, referCode string) (*user.User, error) {
	var data user.User
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Where("refer_code = ?", referCode).First(&data).Error
	})
	return &data, err
}

func (m *userRepo) FindOneSubscribeDetailsById(ctx context.Context, id int64) (*user.SubscribeDetails, error) {
	var data user.SubscribeDetails
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Preload("Subscribe").Preload("User").Where("id = ?", id).First(&data).Error
	})
	return &data, err
}

func userDateBucketExpr(db *gorm.DB, column, bucket string) string {
	if db.Dialector.Name() == "postgres" {
		if bucket == "month" {
			return fmt.Sprintf("TO_CHAR(%s, 'YYYY-MM')", column)
		}
		return fmt.Sprintf("TO_CHAR(%s, 'YYYY-MM-DD')", column)
	}
	if bucket == "month" {
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m')", column)
	}
	return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
}

// QueryDailyUserStatisticsList Query daily user statistics list for the current month (from 1st to current date)
func (m *userRepo) QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error) {
	var results []user.UserStatisticsWithDate

	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v interface{}) error {
		firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
		orderDateExpr := userDateBucketExpr(conn, "created_at", "day")
		userCreatedAt := userColumn(conn, "created_at")
		userDateExpr := userDateBucketExpr(conn, userCreatedAt, "day")

		// 子查询：统计每天的新用户订单数量
		newOrderSub := conn.Model(&order.Order{}).
			Select(fmt.Sprintf("%s AS date, COUNT(DISTINCT user_id) AS new_order_users", orderDateExpr)).
			Where("is_new = ? AND created_at BETWEEN ? AND ? AND status IN ?", true, firstDay, date, []int64{2, 5}).
			Group(orderDateExpr)

		// 子查询：统计每天的续费订单数量
		renewalOrderSub := conn.Model(&order.Order{}).
			Select(fmt.Sprintf("%s AS date, COUNT(DISTINCT user_id) AS renewal_order_users", orderDateExpr)).
			Where("is_new = ? AND created_at BETWEEN ? AND ? AND status IN ?", false, firstDay, date, []int64{2, 5}).
			Group(orderDateExpr)

		return conn.Model(&user.User{}).
			Select(fmt.Sprintf(`
                %s AS date,
                COUNT(*) AS register,
                COALESCE(MAX(n.new_order_users), 0) AS new_order_users,
                COALESCE(MAX(r.renewal_order_users), 0) AS renewal_order_users
            `, userDateExpr)).
			Joins("LEFT JOIN (?) AS n ON "+userDateExpr+" = n.date", newOrderSub).
			Joins("LEFT JOIN (?) AS r ON "+userDateExpr+" = r.date", renewalOrderSub).
			Where(userCreatedAt+" BETWEEN ? AND ?", firstDay, date).
			Group(userDateExpr).
			Order("date ASC").
			Scan(v).Error
	})

	return results, err
}

// QueryMonthlyUserStatisticsList Query monthly user statistics list for the past 6 months
func (m *userRepo) QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error) {
	var results []user.UserStatisticsWithDate

	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v interface{}) error {
		// 获取 6 个月前的日期
		sixMonthsAgo := date.AddDate(0, -5, 0)
		orderDateExpr := userDateBucketExpr(conn, "created_at", "month")
		userCreatedAt := userColumn(conn, "created_at")
		userDateExpr := userDateBucketExpr(conn, userCreatedAt, "month")

		// 子查询：每月新订单用户数量
		newOrderSub := conn.Model(&order.Order{}).
			Select(fmt.Sprintf("%s AS date, COUNT(DISTINCT user_id) AS new_order_users", orderDateExpr)).
			Where("is_new = ? AND created_at >= ? AND status IN ?", true, sixMonthsAgo, []int64{2, 5}).
			Group(orderDateExpr)

		// 子查询：每月续费订单用户数量
		renewalOrderSub := conn.Model(&order.Order{}).
			Select(fmt.Sprintf("%s AS date, COUNT(DISTINCT user_id) AS renewal_order_users", orderDateExpr)).
			Where("is_new = ? AND created_at >= ? AND status IN ?", false, sixMonthsAgo, []int64{2, 5}).
			Group(orderDateExpr)

		return conn.Model(&user.User{}).
			Select(fmt.Sprintf(`
				%s AS date,
				COUNT(*) AS register,
				COALESCE(MAX(n.new_order_users), 0) AS new_order_users,
				COALESCE(MAX(r.renewal_order_users), 0) AS renewal_order_users
			`, userDateExpr)).
			Joins("LEFT JOIN (?) AS n ON "+userDateExpr+" = n.date", newOrderSub).
			Joins("LEFT JOIN (?) AS r ON "+userDateExpr+" = r.date", renewalOrderSub).
			Where(userCreatedAt+" >= ?", sixMonthsAgo).
			Group(userDateExpr).
			Order("date ASC").
			Scan(v).Error
	})

	return results, err
}

// --- auth methods ---

func (m *userRepo) FindUserAuthMethods(ctx context.Context, userId int64) ([]*user.AuthMethods, error) {
	var data []*user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.AuthMethods{}).Where("user_id = ?", userId).Find(&data).Error
	})
	return data, err
}

func (m *userRepo) FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error) {
	var data user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		resolved, err := findUserAuthMethodByIdentifier(conn, method, openID)
		if err != nil {
			return err
		}
		data = *resolved
		return nil
	})
	return &data, err
}

func (m *userRepo) FindUserAuthMethodByPlatform(ctx context.Context, userId int64, platform string) (*user.AuthMethods, error) {
	var data user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", userId, platform).First(&data).Error
	})
	return &data, err
}

func (m *userRepo) InsertUserAuthMethods(ctx context.Context, data *user.AuthMethods, tx ...*gorm.DB) error {
	identifier, err := canonicalAuthIdentifier(data.AuthType, data.AuthIdentifier)
	if err != nil {
		return err
	}
	data.AuthIdentifier = identifier
	u, err := m.FindOne(ctx, data.UserId)
	if err != nil {
		return err
	}

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		if err = guardEmailIdentityWrite(conn, data); err != nil {
			return err
		}
		if err = conn.Model(&user.AuthMethods{}).Create(data).Error; err != nil {
			return err
		}
		return m.ClearUserCache(ctx, u)
	})
}

func (m *userRepo) UpdateUserAuthMethods(ctx context.Context, data *user.AuthMethods, tx ...*gorm.DB) error {
	identifier, err := canonicalAuthIdentifier(data.AuthType, data.AuthIdentifier)
	if err != nil {
		return err
	}
	data.AuthIdentifier = identifier
	u, err := m.FindOne(ctx, data.UserId)
	if err != nil {
		return err
	}

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		if err = guardEmailIdentityWrite(conn, data); err != nil {
			return err
		}
		err = conn.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", data.UserId, data.AuthType).Save(data).Error
		if err != nil {
			return err
		}
		return m.ClearUserCache(ctx, u)
	})
}

func (m *userRepo) DeleteUserAuthMethods(ctx context.Context, userId int64, platform string, tx ...*gorm.DB) error {
	u, err := m.FindOne(ctx, userId)
	if err != nil {
		return err
	}
	defer func() {
		if err = m.ClearUserCache(context.Background(), u); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", userId, platform).Delete(&user.AuthMethods{}).Error
	})
}

func (m *userRepo) UpdateUserAuthMethodOwner(ctx context.Context, authType, identifier string, userId int64, tx ...*gorm.DB) error {
	authMethod, err := m.FindUserAuthMethodByOpenID(ctx, authType, identifier)
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
		if err = m.ClearUserCache(context.Background(), oldUser, newUser); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.AuthMethods{}).
			Where("id = ?", authMethod.Id).
			Update("user_id", userId).Error
	})
}

func (m *userRepo) DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, identifier string, tx ...*gorm.DB) error {
	authMethod, err := m.FindUserAuthMethodByOpenID(ctx, authType, identifier)
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
		if err = m.ClearUserCache(context.Background(), u); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.AuthMethods{}).
			Where("id = ?", authMethod.Id).
			Delete(&user.AuthMethods{}).Error
	})
}

func (m *userRepo) UpsertUserAuthMethod(ctx context.Context, data *user.AuthMethods) error {
	current, err := m.FindUserAuthMethodByPlatform(ctx, data.UserId, data.AuthType)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m.InsertUserAuthMethods(ctx, data)
		}
		return err
	}
	current.AuthIdentifier = data.AuthIdentifier
	return m.UpdateUserAuthMethods(ctx, current)
}

func (m *userRepo) FindUserAuthMethodByUserId(ctx context.Context, method string, userId int64) (*user.AuthMethods, error) {
	var data user.AuthMethods
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.AuthMethods{}).Where("auth_type = ? AND user_id = ?", method, userId).First(&data).Error
	})
	return &data, err
}

// --- subscribe ---

func (m *userRepo) UpdateUserSubscribeCache(ctx context.Context, data *user.Subscribe) error {
	return m.ClearSubscribeCache(ctx, data)
}

// QueryActiveSubscriptions returns the number of active subscriptions.
func (m *userRepo) QueryActiveSubscriptions(ctx context.Context, subscribeId ...int64) (map[int64]int64, error) {
	type SubscriptionCount struct {
		SubscribeId int64
		Total       int64
	}
	var result []SubscriptionCount
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).
			Where("subscribe_id IN ? AND status IN ?", subscribeId, []int64{1, 0}).
			Select("subscribe_id, COUNT(id) as total").
			Group("subscribe_id").
			Scan(&result).
			Error
	})

	if err != nil {
		return nil, err
	}

	resultMap := make(map[int64]int64)
	for _, item := range result {
		resultMap[item.SubscribeId] = item.Total
	}

	return resultMap, nil
}

func (m *userRepo) FindOneSubscribeByOrderId(ctx context.Context, orderId int64) (*user.Subscribe, error) {
	var data user.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Where("order_id = ?", orderId).First(&data).Error
	})
	return &data, err
}

func (m *userRepo) FindOneSubscribe(ctx context.Context, id int64) (*user.Subscribe, error) {
	var data user.Subscribe
	key := fmt.Sprintf("%s%d", cacheUserSubscribeIdPrefix, id)
	err := m.QueryCtx(ctx, &data, key, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Where("id = ?", id).First(&data).Error
	})
	return &data, err
}

func (m *userRepo) FindUsersSubscribeBySubscribeId(ctx context.Context, subscribeId int64) ([]*user.Subscribe, error) {
	var data []*user.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Where("subscribe_id = ? AND status IN ?", subscribeId, []int64{1, 0}).Find(v).Error
	})
	return data, err
}

func (m *userRepo) FindUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*user.Subscribe, error) {
	var data []*user.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&user.Subscribe{})
		if len(status) > 0 {
			conn = conn.Where("status IN ?", status)
		}
		return conn.Find(v).Error
	})
	return data, err
}

func (m *userRepo) ActivatePendingSubscribesBySubscribeId(ctx context.Context, subscribeId int64) error {
	var pending []*user.Subscribe
	err := m.QueryNoCacheCtx(ctx, &pending, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Where("subscribe_id = ? AND status = ?", subscribeId, 0).Find(v).Error
	})
	if err != nil || len(pending) == 0 {
		return err
	}

	cacheKeys := make([]string, 0)
	for _, sub := range pending {
		cacheKeys = append(cacheKeys, sub.GetCacheKeys()...)
	}

	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&user.Subscribe{}).Where("subscribe_id = ? AND status = ?", subscribeId, 0).Update("status", 1).Error
	}, cacheKeys...)
}

func (m *userRepo) CountUserSubscribesBySubscribeIdAndStatus(ctx context.Context, subscribeId int64, status ...int64) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&user.Subscribe{}).Where("subscribe_id = ?", subscribeId)
		if len(status) > 0 {
			conn = conn.Where("status IN ?", status)
		}
		return conn.Count(&total).Error
	})
	return total, err
}

func (m *userRepo) CountUserSubscribesByUserAndSubscribe(ctx context.Context, userId, subscribeId int64) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).
			Where("user_id = ? AND subscribe_id = ?", userId, subscribeId).
			Count(&total).Error
	})
	return total, err
}

// QueryUserSubscribe returns a list of records that meet the conditions.
func (m *userRepo) QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*user.SubscribeDetails, error) {
	var list []*user.SubscribeDetails
	key := fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, userId)
	err := m.QueryCtx(ctx, &list, key, func(conn *gorm.DB, v interface{}) error {
		// 获取当前时间
		now := timeutil.Now()
		// 获取当前时间向前推 7 天
		sevenDaysAgo := timeutil.Now().Add(-7 * 24 * time.Hour)
		// 基础条件查询
		conn = conn.Model(&user.Subscribe{}).Where("user_id = ?", userId)
		if len(status) > 0 {
			conn = conn.Where("status IN ?", status)
		}
		// 订阅过期时间大于当前时间或者订阅结束时间大于当前时间
		return conn.Where("expire_time > ? OR finished_at >= ? OR expire_time = ?", now, sevenDaysAgo, time.UnixMilli(0)).
			Preload("Subscribe").
			Find(&list).Error
	})
	return list, err
}

// FindOneUserSubscribe  finds a subscribeDetails by id.
func (m *userRepo) FindOneUserSubscribe(ctx context.Context, id int64) (subscribeDetails *user.SubscribeDetails, err error) {
	//TODO cache
	//key := fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, userId)
	subscribeDetails = new(user.SubscribeDetails)
	err = m.QueryNoCacheCtx(ctx, subscribeDetails, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Preload("Subscribe").Where("id = ?", id).First(v).Error
	})
	return
}

func (m *userRepo) UpdateUserSubscribeWithTraffic(ctx context.Context, id, download, upload int64, tx ...*gorm.DB) error {
	sub, err := m.FindOneSubscribe(ctx, id)
	if err != nil {
		return err
	}

	// 使用 defer 确保更新后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCache(ctx, sub); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.Subscribe{}).Where("id = ?", id).Updates(map[string]interface{}{
			"download": gorm.Expr("download + ?", download),
			"upload":   gorm.Expr("upload + ?", upload),
		}).Error
	})
}

func (m *userRepo) BatchUpdateUserSubscribeWithTraffic(ctx context.Context, deltas []trafficEntity.SubscribeTrafficDelta, tx ...*gorm.DB) error {
	deltas = mergeSubscribeTrafficDeltas(deltas)
	if len(deltas) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(deltas))
	for _, delta := range deltas {
		ids = append(ids, delta.SubscribeId)
	}
	subs, err := m.FindSubscribesByIds(ctx, ids)
	if err != nil {
		return err
	}

	defer func() {
		_ = m.ClearSubscribeCache(ctx, subs...)
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		downloadExpr, downloadArgs := userSubscribeTrafficIncrementExpr(conn, "download", deltas)
		uploadExpr, uploadArgs := userSubscribeTrafficIncrementExpr(conn, "upload", deltas)
		return conn.Model(&user.Subscribe{}).Where("id IN ?", ids).Updates(map[string]interface{}{
			"download": gorm.Expr(downloadExpr, downloadArgs...),
			"upload":   gorm.Expr(uploadExpr, uploadArgs...),
		}).Error
	})
}

func mergeSubscribeTrafficDeltas(deltas []trafficEntity.SubscribeTrafficDelta) []trafficEntity.SubscribeTrafficDelta {
	if len(deltas) == 0 {
		return nil
	}
	merged := make(map[int64]trafficEntity.SubscribeTrafficDelta, len(deltas))
	for _, delta := range deltas {
		if delta.SubscribeId <= 0 {
			continue
		}
		current := merged[delta.SubscribeId]
		current.SubscribeId = delta.SubscribeId
		current.Download += delta.Download
		current.Upload += delta.Upload
		merged[delta.SubscribeId] = current
	}
	result := make([]trafficEntity.SubscribeTrafficDelta, 0, len(merged))
	for _, delta := range merged {
		result = append(result, delta)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].SubscribeId < result[j].SubscribeId
	})
	return result
}

func userSubscribeTrafficIncrementExpr(db *gorm.DB, column string, deltas []trafficEntity.SubscribeTrafficDelta) (string, []interface{}) {
	idColumn := userSubscribeColumn(db, "id")
	targetColumn := userSubscribeColumn(db, column)
	parts := make([]string, 0, len(deltas))
	args := make([]interface{}, 0, len(deltas)*2)
	for _, delta := range deltas {
		parts = append(parts, "WHEN ? THEN ?")
		args = append(args, delta.SubscribeId)
		if column == "download" {
			args = append(args, delta.Download)
		} else {
			args = append(args, delta.Upload)
		}
	}
	return fmt.Sprintf("%s + CASE %s %s ELSE 0 END", targetColumn, idColumn, strings.Join(parts, " ")), args
}

// FindOneSubscribeByToken  finds a record by token.
func (m *userRepo) FindOneSubscribeByToken(ctx context.Context, token string) (*user.Subscribe, error) {
	var data user.Subscribe
	key := fmt.Sprintf("%s%s", cacheUserSubscribeTokenPrefix, token)
	err := m.QueryCtx(ctx, &data, key, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Where("token = ?", token).First(&data).Error
	})
	return &data, err
}

// UpdateSubscribe updates a record.
func (m *userRepo) UpdateSubscribe(ctx context.Context, data *user.Subscribe, tx ...*gorm.DB) error {
	old, err := m.FindOneSubscribe(ctx, data.Id)
	if err != nil {
		return err
	}

	// 使用 defer 确保更新后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCache(ctx, old, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.Subscribe{}).Where("id = ?", data.Id).Save(data).Error
	})
}

// DeleteSubscribe deletes a record.
func (m *userRepo) DeleteSubscribe(ctx context.Context, token string, tx ...*gorm.DB) error {
	data, err := m.FindOneSubscribeByToken(ctx, token)
	if err != nil {
		return err
	}

	// 使用 defer 确保删除后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCache(ctx, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Where("token = ?", token).Delete(&user.Subscribe{}).Error
	})
}

// InsertSubscribe insert Subscribe into the database.
func (m *userRepo) InsertSubscribe(ctx context.Context, data *user.Subscribe, tx ...*gorm.DB) error {
	// 使用 defer 确保插入后清理相关缓存
	defer func() {
		if clearErr := m.ClearSubscribeCache(ctx, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(data).Error
	})
}

func (m *userRepo) DeleteSubscribeById(ctx context.Context, id int64, tx ...*gorm.DB) error {
	data, err := m.FindOneSubscribe(ctx, id)
	if err != nil {
		return err
	}

	// 使用 defer 确保删除后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCache(ctx, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Where("id = ?", id).Delete(&user.Subscribe{}).Error
	})
}

func (m *userRepo) ClearSubscribeCache(ctx context.Context, data ...*user.Subscribe) error {
	if len(data) == 0 {
		return nil
	}
	var keys []string
	for _, s := range data {
		if s != nil {
			keys = append(keys, s.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

// --- device ---

func (m *userRepo) FindOneDevice(ctx context.Context, id int64) (*user.Device, error) {
	deviceIdKey := fmt.Sprintf("%s%v", cacheUserDeviceIdPrefix, id)
	var resp user.Device
	err := m.QueryCtx(ctx, &resp, deviceIdKey, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Device{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *userRepo) FindOneDeviceByIdentifier(ctx context.Context, id string) (*user.Device, error) {
	deviceIdKey := fmt.Sprintf("%s%v", cacheUserDeviceNumberPrefix, id)
	var resp user.Device
	err := m.QueryCtx(ctx, &resp, deviceIdKey, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Device{}).Where("identifier = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

// QueryDevicePageList  returns a list of records that meet the conditions.
func (m *userRepo) QueryDevicePageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*user.Device, int64, error) {
	var list []*user.Device
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Device{}).Where("user_id = ? and subscribe_id = ?", userId, subscribeId).Count(&total).Limit(size).Offset((page - 1) * size).Find(&list).Error
	})
	return list, total, err
}

// QueryDeviceList  returns a list of records that meet the conditions.
func (m *userRepo) QueryDeviceList(ctx context.Context, userId int64) ([]*user.Device, int64, error) {
	var list []*user.Device
	var total int64
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Device{}).Where("user_id = ?", userId).Count(&total).Find(&list).Error
	})
	return list, total, err
}

func (m *userRepo) UpdateDevice(ctx context.Context, data *user.Device, tx ...*gorm.DB) error {
	old, err := m.FindOneDevice(ctx, data.Id)
	if err != nil {
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Save(data).Error
	}, old.GetCacheKeys()...)
	return err
}

func (m *userRepo) DeleteDevice(ctx context.Context, id int64, tx ...*gorm.DB) error {
	data, err := m.FindOneDevice(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Delete(&user.Device{}, id).Error
	}, data.GetCacheKeys()...)
	return err
}

func (m *userRepo) InsertDevice(ctx context.Context, data *user.Device, tx ...*gorm.DB) error {
	defer func() {
		if clearErr := m.ClearDeviceCache(ctx, data); clearErr != nil {
			// log cache clear error
		}
	}()

	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(data).Error
	})
}

func (m *userRepo) FindDeviceOnlineRecord(ctx context.Context, userId int64, startTime, endTime string) (*user.DeviceOnlineRecord, error) {
	var record user.DeviceOnlineRecord
	err := m.QueryNoCacheCtx(ctx, &record, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.DeviceOnlineRecord{}).
			Where("user_id = ? AND create_at >= ? AND create_at < ?", userId, startTime, endTime).
			First(&record).Error
	})
	return &record, err
}

func (m *userRepo) InsertDeviceOnlineRecord(ctx context.Context, data *user.DeviceOnlineRecord, tx ...*gorm.DB) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(data).Error
	})
}

// --- withdrawal ---

func (m *userRepo) InsertWithdrawal(ctx context.Context, data *user.Withdrawal, tx ...*gorm.DB) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(data).Error
	})
}

// --- affiliate / batch / multi-id queries ---

func (m *userRepo) CountAffiliates(ctx context.Context, refererId int64) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Where("referer_id = ?", refererId).Count(&total).Error
	})
	return total, err
}

func (m *userRepo) QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*user.User, int64, error) {
	var list []*user.User
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
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

func (m *userRepo) FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error) {
	var users []*user.User
	if len(ids) == 0 {
		return users, nil
	}
	err := m.QueryNoCacheCtx(ctx, &users, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.User{}).Where("id IN ?", ids).Find(&users).Error
	})
	return users, err
}

func (m *userRepo) FindSubscribesByIds(ctx context.Context, ids []int64) ([]*user.Subscribe, error) {
	var subscribes []*user.Subscribe
	if len(ids) == 0 {
		return subscribes, nil
	}
	err := m.QueryNoCacheCtx(ctx, &subscribes, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).Where("id IN ?", ids).Find(&subscribes).Error
	})
	return subscribes, err
}

// --- subscription checks (expired / traffic exceeded) ---

func (m *userRepo) FindTrafficExceededSubscribes(ctx context.Context) ([]*user.Subscribe, error) {
	var list []*user.Subscribe
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).
			Where("upload + download >= traffic AND status IN ? AND traffic > 0", []int64{0, 1}).
			Find(&list).Error
	})
	return list, err
}

func (m *userRepo) FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*user.Subscribe, error) {
	var list []*user.Subscribe
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&user.Subscribe{}).
			Where("status IN ? AND expire_time < ? AND expire_time != ? AND finished_at IS NULL", []int64{0, 1}, now, time.UnixMilli(0)).
			Find(&list).Error
	})
	return list, err
}

func (m *userRepo) MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time, tx ...*gorm.DB) error {
	if len(ids) == 0 {
		return nil
	}
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.Subscribe{}).Where("id IN ?", ids).Updates(map[string]interface{}{
			"status":      status,
			"finished_at": finishedAt,
		}).Error
	})
}

// --- reset traffic ---

func (m *userRepo) QueryMonthlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error) {
	var ids []int64
	if len(subscribeIds) == 0 {
		return ids, nil
	}
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v interface{}) error {
		return userMonthlyResetSubscribeQuery(conn, subscribeIds, now).Find(&ids).Error
	})
	return ids, err
}

func (m *userRepo) QueryFirstResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error) {
	var ids []int64
	if len(subscribeIds) == 0 {
		return ids, nil
	}
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v interface{}) error {
		return userResettableSubscribeQuery(conn, subscribeIds, now).Find(&ids).Error
	})
	return ids, err
}

func (m *userRepo) QueryYearlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error) {
	var ids []int64
	if len(subscribeIds) == 0 {
		return ids, nil
	}
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v interface{}) error {
		return userYearlyResetSubscribeQuery(conn, subscribeIds, now).Find(&ids).Error
	})
	return ids, err
}

func (m *userRepo) ResetSubscribeTrafficByIds(ctx context.Context, ids []int64, tx ...*gorm.DB) error {
	if len(ids) == 0 {
		return nil
	}
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&user.Subscribe{}).Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"upload":      0,
				"download":    0,
				"status":      1,
				"finished_at": nil,
			}).Error
	})
}

func userExtractColumnDatePart(db *gorm.DB, column, part string) string {
	if db.Dialector.Name() == "postgres" {
		return fmt.Sprintf("EXTRACT(%s FROM %s)", part, column)
	}
	switch part {
	case "month":
		return fmt.Sprintf("MONTH(%s)", column)
	default:
		return fmt.Sprintf("DAY(%s)", column)
	}
}

func userMonthlyResetSubscribeQuery(conn *gorm.DB, subscribeIds []int64, now time.Time) *gorm.DB {
	query := userResettableSubscribeQuery(conn, subscribeIds, now)
	condition, args := userMonthlyResetDateCondition(conn, now)
	return query.Where(condition, args...)
}

func userYearlyResetSubscribeQuery(conn *gorm.DB, subscribeIds []int64, now time.Time) *gorm.DB {
	query := userResettableSubscribeQuery(conn, subscribeIds, now)
	condition, args := userYearlyResetDateCondition(conn, now)
	return query.Where(condition, args...)
}

func userResettableSubscribeQuery(conn *gorm.DB, subscribeIds []int64, now time.Time) *gorm.DB {
	return conn.Model(&user.Subscribe{}).Select("id").
		Where("subscribe_id IN ?", subscribeIds).
		Where("status IN ?", []int64{1, 2}).
		Where("start_time <= ?", now).
		Where("(expire_time IS NULL OR expire_time = ? OR expire_time > ?)", time.UnixMilli(0), now)
}

func userMonthlyResetDateCondition(db *gorm.DB, now time.Time) (string, []interface{}) {
	dayExpr := userExtractColumnDatePart(db, "start_time", "day")
	if userIsLastDayOfMonth(now) {
		return dayExpr + " >= ?", []interface{}{now.Day()}
	}
	return dayExpr + " = ?", []interface{}{now.Day()}
}

func userYearlyResetDateCondition(db *gorm.DB, now time.Time) (string, []interface{}) {
	monthExpr := userExtractColumnDatePart(db, "start_time", "month")
	dayExpr := userExtractColumnDatePart(db, "start_time", "day")
	if now.Month() == time.February && now.Day() == 28 && !userIsLeapYear(now.Year()) {
		return fmt.Sprintf("%s = ? AND %s IN ?", monthExpr, dayExpr), []interface{}{int(time.February), []int{28, 29}}
	}
	return fmt.Sprintf("%s = ? AND %s = ?", monthExpr, dayExpr), []interface{}{int(now.Month()), now.Day()}
}

func userIsLastDayOfMonth(t time.Time) bool {
	return t.AddDate(0, 0, 1).Month() != t.Month()
}

func userIsLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// --- cache helpers ---

func (m *userRepo) ClearUserCache(ctx context.Context, users ...*user.User) error {
	if len(users) == 0 {
		return nil
	}
	var keys []string
	for _, u := range users {
		if u != nil {
			keys = append(keys, u.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *userRepo) ClearDeviceCache(ctx context.Context, devices ...*user.Device) error {
	if len(devices) == 0 {
		return nil
	}
	var keys []string
	for _, d := range devices {
		if d != nil {
			keys = append(keys, d.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *userRepo) ClearAuthMethodCache(ctx context.Context, authMethods ...*user.AuthMethods) error {
	if len(authMethods) == 0 {
		return nil
	}
	var keys []string
	for _, a := range authMethods {
		if a != nil {
			keys = append(keys, a.GetCacheKeys()...)
		}
	}
	return m.CachedConn.DelCacheCtx(ctx, keys...)
}

func (m *userRepo) BatchClearRelatedCache(ctx context.Context, u *user.User) error {
	if u == nil {
		return nil
	}
	var allKeys []string
	allKeys = append(allKeys, u.GetCacheKeys()...)

	for _, auth := range u.AuthMethods {
		allKeys = append(allKeys, auth.GetCacheKeys()...)
	}

	for _, device := range u.UserDevices {
		allKeys = append(allKeys, device.GetCacheKeys()...)
	}

	subscribes, err := m.QueryUserSubscribe(ctx, u.Id)
	if err != nil {
		logger.Errorf("failed to query user subscribes for cache clearing: %v", err)
	} else {
		for _, sub := range subscribes {
			subModel := &user.Subscribe{
				Id:          sub.Id,
				UserId:      sub.UserId,
				Token:       sub.Token,
				SubscribeId: sub.SubscribeId,
			}
			allKeys = append(allKeys, subModel.GetCacheKeys()...)
		}
	}

	return m.CachedConn.DelCacheCtx(ctx, allKeys...)
}
