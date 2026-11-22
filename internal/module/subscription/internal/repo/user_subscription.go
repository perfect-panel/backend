// Package repo holds the subscription module's repository implementations:
// plans and groups, user subscriptions with their traffic accounting and
// cache bridges, client applications and provider entitlements (ADR-001
// step-6 preparation).
package repo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cache key prefixes shared with the usersub entity's key derivation.
const (
	cacheUserSubscribeTokenPrefix = "cache:user:subscribe:token:" //nolint:gosec // G101: a cache key prefix, not a credential
	// v3 stores the complete, status-unfiltered subscription history.
	// Status-specific callers filter this shared value in memory so cache
	// entries cannot collide.
	cacheUserSubscribeUserPrefix = "cache:user:subscribe:user:v3:"
	cacheUserSubscribeIdPrefix   = "cache:user:subscribe:id:"
)

// batchUpdateSize bounds the rows one batched UPDATE names, keeping the
// statement and its bound parameters well inside every driver's limits.
const batchUpdateSize = 500

func userSubscribeColumn(db *gorm.DB, column string) string {
	table := (&usersub.Subscribe{}).TableName()
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Column{Table: table, Name: column})
	}
	return table + "." + column
}

// UserSubscriptionRepo is the subscription-owned implementation of the
// user-subscription and traffic contracts (plus the identity bundle's cache
// bridge): user_subscribe rows, traffic accounting, expiry/reset sweeps.
type UserSubscriptionRepo struct {
	cache.CachedConn
}

var (
	_ repository.UserSubscriptionRepo    = (*UserSubscriptionRepo)(nil)
	_ repository.SubscriptionTrafficRepo = (*UserSubscriptionRepo)(nil)
	_ repository.SubscriptionCacheBridge = (*UserSubscriptionRepo)(nil)
	_ repository.SubscriptionScopeBridge = (*UserSubscriptionRepo)(nil)
)

// SubscriptionUserIDs resolves the distinct user ids whose subscription
// rows match the filter (repository.SubscriptionScopeBridge): the identity
// bundle's admin user filter and email-recipient scopes consume the id
// list instead of querying this table from identity SQL.
func (m *UserSubscriptionRepo) SubscriptionUserIDs(ctx context.Context, filter repository.SubscriptionUserFilter) ([]int64, error) {
	ids := make([]int64, 0)
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v any) error {
		q := conn.Model(&usersub.Subscribe{}).Distinct("user_id")
		if filter.UserSubscribeID != nil {
			q = q.Where("id = ?", *filter.UserSubscribeID)
		}
		if filter.SubscribeID != nil {
			q = q.Where("subscribe_id = ?", *filter.SubscribeID)
		}
		if filter.Token != "" {
			q = q.Where("(token = ? OR uuid = ?)", filter.Token, filter.Token)
		}
		if filter.Statuses != nil {
			q = q.Where("status IN ?", filter.Statuses)
		}
		return q.Pluck("user_id", v).Error
	})
	return ids, err
}

// NewUserSubscriptionRepo builds the module-owned implementation over the
// shared cached connection.
func NewUserSubscriptionRepo(conn cache.CachedConn) *UserSubscriptionRepo {
	return &UserSubscriptionRepo{CachedConn: conn}
}

// userSerial is the per-user serialization row for subscription-creating
// flows.
type userSerial struct {
	UserId int64 `gorm:"primaryKey"`
}

func (userSerial) TableName() string {
	return "subscription_user_serial"
}

// LockUserSerial seeds (once) and locks the user's serial row inside the
// current transaction, serializing quota checks and subscription creation
// per user within the subscription domain.
func (m *UserSubscriptionRepo) LockUserSerial(ctx context.Context, userID int64) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		if err := conn.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&userSerial{UserId: userID}).Error; err != nil {
			return err
		}
		var row userSerial
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&row).Error
	})
}

// --- subscribe ---

func (m *UserSubscriptionRepo) UpdateUserSubscribeCache(ctx context.Context, data *usersub.Subscribe) error {
	return m.ClearSubscribeCache(ctx, data)
}

// QueryActiveSubscriptions counts the live subscriptions of each plan.
func (m *UserSubscriptionRepo) QueryActiveSubscriptions(ctx context.Context, subscribeId ...int64) (map[int64]int64, error) {
	type SubscriptionCount struct {
		SubscribeId int64
		Total       int64
	}
	var result []SubscriptionCount
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).
			Where("subscribe_id IN ? AND status IN ?", subscribeId, usersub.LiveStatuses.Values()).
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

func (m *UserSubscriptionRepo) FindOneSubscribeByOrderId(ctx context.Context, orderId int64) (*usersub.Subscribe, error) {
	var data usersub.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Where("order_id = ?", orderId).First(&data).Error
	})
	return &data, err
}

func (m *UserSubscriptionRepo) FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	var data usersub.Subscribe
	key := fmt.Sprintf("%s%d", cacheUserSubscribeIdPrefix, id)
	err := m.QueryCtx(ctx, &data, key, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Where("id = ?", id).First(&data).Error
	})
	return &data, err
}

func (m *UserSubscriptionRepo) FindOneSubscribeForUpdate(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	var data usersub.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Model(&usersub.Subscribe{}).
			Where("id = ?", id).
			First(v).Error
	})
	return &data, err
}

// FindUsersSubscribeBySubscribeIds returns the plans' subscriptions a node
// may serve (see servableSubscribes).
func (m *UserSubscriptionRepo) FindUsersSubscribeBySubscribeIds(ctx context.Context, subscribeIds []int64) ([]*usersub.Subscribe, error) {
	var data []*usersub.Subscribe
	if len(subscribeIds) == 0 {
		return data, nil
	}
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return servableSubscribes(conn, subscribeIds, timeutil.Now()).Find(v).Error
	})
	return data, err
}

// servableSubscribes selects the plans' subscriptions a node may serve now:
// usersub.ServableCondition, the SQL form of the rule delivery, the
// storefront and the edge manifest apply. Access ends when a subscription is
// due to finish rather than when the lifecycle sweep next runs. Not built on
// activeLifecycleSubscribes: its finished_at term exists for the sweep's
// partial index, which this plan-driven query does not use, and would change
// who is served.
func servableSubscribes(conn *gorm.DB, subscribeIds []int64, now time.Time) *gorm.DB {
	condition, args := usersub.ServableCondition(now)
	return conn.Model(&usersub.Subscribe{}).
		Where("subscribe_id IN ?", subscribeIds).
		Where(condition, args...).
		Order("subscribe_id ASC, id ASC")
}

func (m *UserSubscriptionRepo) FindUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*usersub.Subscribe, error) {
	var data []*usersub.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		conn = conn.Model(&usersub.Subscribe{})
		if len(status) > 0 {
			conn = conn.Where("status IN ?", status)
		}
		return conn.Find(v).Error
	})
	return data, err
}

func (m *UserSubscriptionRepo) CountUserSubscribesBySubscribeIdAndStatus(ctx context.Context, subscribeId int64, status ...int64) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		conn = conn.Model(&usersub.Subscribe{}).Where("subscribe_id = ?", subscribeId)
		if len(status) > 0 {
			conn = conn.Where("status IN ?", status)
		}
		return conn.Count(&total).Error
	})
	return total, err
}

func (m *UserSubscriptionRepo) CountQuotaConsumingSubscriptions(ctx context.Context, userId, subscribeId int64) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).
			Where("user_id = ? AND subscribe_id = ? AND status <> ?", userId, subscribeId, usersub.SubscribeStatusDeducted).
			Count(&total).Error
	})
	return total, err
}

func (m *UserSubscriptionRepo) HasBlockingSubscription(ctx context.Context, userId int64) (bool, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).
			Where("user_id = ? AND status <> ?", userId, usersub.SubscribeStatusDeducted).
			Count(&total).Error
	})
	return total > 0, err
}

// activeFirstOrder lists Active subscriptions before the others.
var activeFirstOrder = fmt.Sprintf("CASE WHEN status = %d THEN 0 ELSE 1 END ASC", usersub.SubscribeStatusActive)

// QueryUserSubscribe returns the complete cached subscription history for a user.
func (m *UserSubscriptionRepo) QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*usersub.SubscribeDetails, error) {
	var all []*usersub.SubscribeDetails
	key := fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, userId)
	err := m.QueryCtx(ctx, &all, key, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).
			Where("user_id = ?", userId).
			Preload("Subscribe").
			Order(activeFirstOrder).
			Order("expire_time DESC").
			Order("id DESC").
			Find(&all).Error
	})
	if err != nil {
		return nil, err
	}
	return filterUserSubscribeByStatus(all, status), nil
}

func filterUserSubscribeByStatus(list []*usersub.SubscribeDetails, status []int64) []*usersub.SubscribeDetails {
	if len(status) == 0 {
		return list
	}

	allowed := make(map[int64]struct{}, len(status))
	for _, value := range status {
		allowed[value] = struct{}{}
	}

	filtered := make([]*usersub.SubscribeDetails, 0, len(list))
	for _, item := range list {
		if item == nil {
			continue
		}
		if _, ok := allowed[int64(item.Status)]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// FindOneUserSubscribe  finds a subscribeDetails by id.
func (m *UserSubscriptionRepo) FindOneUserSubscribe(ctx context.Context, id int64) (subscribeDetails *usersub.SubscribeDetails, err error) {
	subscribeDetails = new(usersub.SubscribeDetails)
	err = m.QueryNoCacheCtx(ctx, subscribeDetails, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Preload("Subscribe").Where("id = ?", id).First(v).Error
	})
	return
}

// FindSubscribeDetailsByIds loads the subscriptions with their plans: one
// query for the rows and one for the plans, however many ids there are.
func (m *UserSubscriptionRepo) FindSubscribeDetailsByIds(ctx context.Context, ids []int64) ([]*usersub.SubscribeDetails, error) {
	var list []*usersub.SubscribeDetails
	if len(ids) == 0 {
		return list, nil
	}
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Preload("Subscribe").Where("id IN ?", ids).Find(v).Error
	})
	return list, err
}

// FindSubscribeDetailsByUserIds loads every subscription of the users,
// whatever its status, with its plan: one query for the rows and one for the
// plans, however many users there are.
func (m *UserSubscriptionRepo) FindSubscribeDetailsByUserIds(ctx context.Context, userIds []int64) ([]*usersub.SubscribeDetails, error) {
	var list []*usersub.SubscribeDetails
	if len(userIds) == 0 {
		return list, nil
	}
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Preload("Subscribe").Where("user_id IN ?", userIds).Order("id ASC").Find(v).Error
	})
	return list, err
}

func (m *UserSubscriptionRepo) BatchUpdateUserSubscribeWithTraffic(ctx context.Context, deltas []trafficEntity.SubscribeTrafficDelta) error {
	deltas = mergeSubscribeTrafficDeltas(deltas)
	if len(deltas) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(deltas))
	for _, delta := range deltas {
		ids = append(ids, delta.SubscribeId)
	}
	subs, err := m.findSubscribesByIDsInChunks(ctx, ids)
	if err != nil {
		return err
	}

	// One statement binds about five parameters per subscription (the id
	// list and two CASE branches), so a minute of some 13k subscriptions
	// would exceed the 65535 placeholders MySQL and PostgreSQL allow. The
	// chunks run on the caller's connection, inside its transaction, so the
	// minute still commits as a whole.
	err = m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		for start := 0; start < len(deltas); start += batchUpdateSize {
			chunk := deltas[start:min(start+batchUpdateSize, len(deltas))]
			downloadExpr, downloadArgs := userSubscribeTrafficIncrementExpr(conn, "download", chunk)
			uploadExpr, uploadArgs := userSubscribeTrafficIncrementExpr(conn, "upload", chunk)
			if err := conn.Model(&usersub.Subscribe{}).Where("id IN ?", ids[start:start+len(chunk)]).Updates(map[string]any{
				"download": gorm.Expr(downloadExpr, downloadArgs...),
				"upload":   gorm.Expr(uploadExpr, uploadArgs...),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	m.clearCacheAfterWrite(ctx, "add traffic", subs...)
	return nil
}

// findSubscribesByIDsInChunks reads the rows of the ids batchUpdateSize at a
// time, keeping each id list inside the drivers' placeholder limits.
func (m *UserSubscriptionRepo) findSubscribesByIDsInChunks(ctx context.Context, ids []int64) ([]*usersub.Subscribe, error) {
	subs := make([]*usersub.Subscribe, 0, len(ids))
	for start := 0; start < len(ids); start += batchUpdateSize {
		chunk, err := m.FindSubscribesByIds(ctx, ids[start:min(start+batchUpdateSize, len(ids))])
		if err != nil {
			return nil, err
		}
		subs = append(subs, chunk...)
	}
	return subs, nil
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

func userSubscribeTrafficIncrementExpr(db *gorm.DB, column string, deltas []trafficEntity.SubscribeTrafficDelta) (string, []any) {
	idColumn := userSubscribeColumn(db, "id")
	targetColumn := userSubscribeColumn(db, column)
	parts := make([]string, 0, len(deltas))
	args := make([]any, 0, len(deltas)*2)
	for _, delta := range deltas {
		parts = append(parts, "WHEN ? THEN ?")
		args = append(args, delta.SubscribeId)
		if column == "download" {
			args = append(args, delta.Download)
		} else {
			args = append(args, delta.Upload)
		}
	}
	return fmt.Sprintf(
		"%s + CASE %s %s ELSE %s END",
		targetColumn,
		idColumn,
		strings.Join(parts, " "),
		userSubscribeTrafficZeroExpr(db),
	), args
}

func userSubscribeTrafficZeroExpr(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return "0"
	}
	switch db.Dialector.Name() {
	case "postgres":
		return "0::bigint"
	case "mysql":
		return "CAST(0 AS SIGNED)"
	default:
		return "0"
	}
}

// FindOneSubscribeByToken  finds a record by token.
func (m *UserSubscriptionRepo) FindOneSubscribeByToken(ctx context.Context, token string) (*usersub.Subscribe, error) {
	var data usersub.Subscribe
	key := fmt.Sprintf("%s%s", cacheUserSubscribeTokenPrefix, token)
	err := m.QueryCtx(ctx, &data, key, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Where("token = ?", token).First(&data).Error
	})
	return &data, err
}

func (m *UserSubscriptionRepo) FindOneSubscribeByTokenForUpdate(ctx context.Context, token string) (*usersub.Subscribe, error) {
	var data usersub.Subscribe
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Model(&usersub.Subscribe{}).
			Where("token = ?", token).
			First(v).Error
	})
	return &data, err
}

// errSubscriptionID rejects a write without a subscription id: GORM would
// turn it into an insert or an update of every row.
var errSubscriptionID = errors.New("subscription id is required")

// UpdateSubscribeColumns persists only the given columns of data (plus the
// update timestamp); concurrent writes to every other column survive. A
// provider-managed row accepts only the local-control columns, and becomes
// Active again only inside its provider period: the payment provider owns
// the plan, the order and the term.
func (m *UserSubscriptionRepo) UpdateSubscribeColumns(ctx context.Context, data *usersub.Subscribe, columns ...string) error {
	if len(columns) == 0 {
		return nil
	}
	if data == nil || data.Id == 0 {
		return errSubscriptionID
	}
	now := timeutil.Now()
	reactivates := slices.Contains(columns, "status") && data.Status == usersub.SubscribeStatusActive
	if data.EntitlementSource != "" {
		for _, column := range columns {
			if !slices.Contains(usersub.LocalControlColumns, column) {
				return usersub.ErrProviderManaged
			}
		}
		if reactivates && (!data.ExpireTime.After(now) || data.StartTime.After(now)) {
			return usersub.ErrProviderManaged
		}
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		query := conn.Model(&usersub.Subscribe{}).
			Where("id = ? AND entitlement_source = ?", data.Id, data.EntitlementSource)
		if data.EntitlementSource != "" && reactivates {
			// The term is the provider's; it may have ended since data was read.
			query = query.Where("expire_time > ? AND start_time <= ?", now, now)
		}
		return query.Select(columns).Updates(data).Error
	}, data.GetCacheKeys()...)
}

// RotateSubscribeCredentials writes each rotation's token and UUID, a batch
// of rows per UPDATE, and invalidates the cache entries of the previous and
// the new credentials.
func (m *UserSubscriptionRepo) RotateSubscribeCredentials(ctx context.Context, rotations []repository.SubscriptionCredentialRotation) error {
	for start := 0; start < len(rotations); start += batchUpdateSize {
		batch := rotations[start:min(start+batchUpdateSize, len(rotations))]
		ids := make([]int64, 0, len(batch))
		keys := make([]string, 0, len(batch)*6)
		for _, rotation := range batch {
			if rotation.Previous == nil || rotation.Previous.Id == 0 {
				return errSubscriptionID
			}
			rotated := *rotation.Previous
			rotated.Token, rotated.UUID = rotation.Token, rotation.UUID
			ids = append(ids, rotated.Id)
			keys = append(keys, rotation.Previous.GetCacheKeys()...)
			keys = append(keys, rotated.GetCacheKeys()...)
		}
		err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
			tokenExpr, tokenArgs := credentialCaseExpr(conn, batch, func(r repository.SubscriptionCredentialRotation) string { return r.Token })
			uuidExpr, uuidArgs := credentialCaseExpr(conn, batch, func(r repository.SubscriptionCredentialRotation) string { return r.UUID })
			return conn.Model(&usersub.Subscribe{}).Where("id IN ?", ids).Updates(map[string]any{
				"token": gorm.Expr(tokenExpr, tokenArgs...),
				"uuid":  gorm.Expr(uuidExpr, uuidArgs...),
			}).Error
		}, keys...)
		if err != nil {
			return err
		}
	}
	return nil
}

// credentialCaseExpr maps each rotated subscription id to its new value.
func credentialCaseExpr(db *gorm.DB, rotations []repository.SubscriptionCredentialRotation, value func(repository.SubscriptionCredentialRotation) string) (string, []any) {
	parts := make([]string, 0, len(rotations))
	args := make([]any, 0, len(rotations)*2)
	for _, rotation := range rotations {
		parts = append(parts, "WHEN ? THEN ?")
		args = append(args, rotation.Previous.Id, value(rotation))
	}
	return fmt.Sprintf("CASE %s %s END", userSubscribeColumn(db, "id"), strings.Join(parts, " ")), args
}

func (m *UserSubscriptionRepo) ApplyEntitlementProjection(ctx context.Context, data *usersub.Subscribe) error {
	if data.EntitlementSource == "" {
		return usersub.ErrProviderManaged
	}
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&usersub.Subscribe{}).Where("id = ? AND entitlement_source = ?", data.Id, data.EntitlementSource).Select("*").Updates(data).Error
	})
}

// InsertSubscribe insert Subscribe into the database.
func (m *UserSubscriptionRepo) InsertSubscribe(ctx context.Context, data *usersub.Subscribe) error {
	err := m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(data).Error
	})
	if err != nil {
		return err
	}
	// The owner's cached subscription list no longer includes every row.
	m.clearCacheAfterWrite(ctx, "insert subscription", data)
	return nil
}

func (m *UserSubscriptionRepo) DeleteSubscribeById(ctx context.Context, id int64) error {
	data, err := m.FindOneSubscribe(ctx, id)
	if err != nil {
		return err
	}
	err = m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Where("id = ?", id).Delete(&usersub.Subscribe{}).Error
	})
	if err != nil {
		return err
	}
	m.clearCacheAfterWrite(ctx, "delete subscription", data)
	return nil
}

func (m *UserSubscriptionRepo) ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error {
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

// clearCacheAfterWrite invalidates the cached rows of a committed write. The
// write stays committed when Redis fails, so the failure is only logged: the
// stale entries expire on their own.
func (m *UserSubscriptionRepo) clearCacheAfterWrite(ctx context.Context, operation string, data ...*usersub.Subscribe) {
	if err := m.ClearSubscribeCache(ctx, data...); err != nil {
		ids := make([]int64, 0, len(data))
		for _, sub := range data {
			if sub != nil {
				ids = append(ids, sub.Id)
			}
		}
		logger.WithContext(ctx).Errorw("[UserSubscriptionRepo] clear subscription cache failed",
			logger.Field("operation", operation),
			logger.Field("user_subscribe_ids", ids),
			logger.Field("error", err.Error()),
		)
	}
}

// --- subscription checks (expired / traffic exceeded) ---

// activeLifecycleCondition deliberately spells the live statuses as SQL
// literals. PostgreSQL can only use the matching partial expiry index when its
// planner can prove the query predicate implies the index predicate; a
// generic prepared plan with status parameters cannot make that proof.
var activeLifecycleCondition = func() string {
	statuses := make([]string, 0, len(usersub.LiveStatuses))
	for _, status := range usersub.LiveStatuses {
		statuses = append(statuses, fmt.Sprint(status))
	}
	return "status IN (" + strings.Join(statuses, ", ") + ") AND finished_at IS NULL"
}()

func activeLifecycleSubscribes(conn *gorm.DB) *gorm.DB {
	return conn.Model(&usersub.Subscribe{}).Where(activeLifecycleCondition)
}

func (m *UserSubscriptionRepo) FindTrafficExceededSubscribes(ctx context.Context) ([]*usersub.Subscribe, error) {
	var list []*usersub.Subscribe
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).
			Where(usersub.TrafficExhaustedCondition).
			Where("status IN ?", usersub.LiveStatuses.Values()).
			Find(&list).Error
	})
	return list, err
}

func (m *UserSubscriptionRepo) FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*usersub.Subscribe, error) {
	var list []*usersub.Subscribe
	condition, args := usersub.ExpiredCondition(now)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return activeLifecycleSubscribes(conn).Where(condition, args...).Find(&list).Error
	})
	return list, err
}

// FindExpiringSubscribes returns the active subscriptions whose expiry falls
// inside the window, so the owner can be reminded before service stops. A
// no-limit subscription never expires (usersub.ExpiringCondition).
func (m *UserSubscriptionRepo) FindExpiringSubscribes(ctx context.Context, from, to time.Time) ([]*usersub.Subscribe, error) {
	var list []*usersub.Subscribe
	expiring, args := usersub.ExpiringCondition(from, to)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return activeLifecycleSubscribes(conn).
			// Provider renewals need provider-specific messaging and prices.
			Where("entitlement_source = ''").
			Where(expiring, args...).
			Find(&list).Error
	})
	return list, err
}

// MarkSubscribesFinished flips the given live subscriptions to status as of
// finishedAt. The expiry or traffic condition is checked again in the
// statement, so a row renewed or reset since it was selected keeps running.
func (m *UserSubscriptionRepo) MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		q := conn.Model(&usersub.Subscribe{}).Where("id IN ? AND status IN ?", ids, usersub.LiveStatuses.Values())
		switch status {
		case usersub.SubscribeStatusExpired:
			condition, args := usersub.ExpiredCondition(finishedAt)
			q = q.Where(condition, args...)
		case usersub.SubscribeStatusFinished:
			q = q.Where(usersub.TrafficExhaustedCondition)
		}
		return q.Updates(map[string]any{
			"status":      status,
			"finished_at": finishedAt,
		}).Error
	})
}

// --- calendar traffic reset ---

// trafficResetCandidates selects the subscriptions a calendar reset for day
// may clear at now: in their term (Active or Finished), started, unexpired,
// and not reset for day yet. A Finished row among them is inside its term, so
// the reset reactivates it (usersub.Subscribe.ReactivatedByTrafficReset).
func trafficResetCandidates(conn *gorm.DB, now, day time.Time) *gorm.DB {
	unexpired, args := usersub.UnexpiredCondition(now)
	return conn.Model(&usersub.Subscribe{}).
		Where("status IN ?", usersub.InTermStatuses.Values()).
		Where("start_time <= ?", now).
		Where(unexpired, args...).
		Where("(traffic_reset_at IS NULL OR traffic_reset_at < ?)", day)
}

func (m *UserSubscriptionRepo) FindTrafficResetCandidates(ctx context.Context, planIDs []int64, now, day time.Time) ([]*usersub.Subscribe, error) {
	var list []*usersub.Subscribe
	if len(planIDs) == 0 {
		return list, nil
	}
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return trafficResetCandidates(conn, now, day).
			Select("id", "user_id", "subscribe_id", "start_time", "traffic_reset_at").
			Where("subscribe_id IN ?", planIDs).
			Order("id ASC").
			Find(v).Error
	})
	return list, err
}

func (m *UserSubscriptionRepo) ResetSubscribeTrafficOnce(ctx context.Context, ids []int64, now, day time.Time) ([]*usersub.Subscribe, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Lock the rows that are still candidates. A concurrent run blocks here
	// and, once this transaction commits, finds them marked.
	var due []*usersub.Subscribe
	err := m.QueryNoCacheCtx(ctx, &due, func(conn *gorm.DB, v any) error {
		return trafficResetCandidates(conn, now, day).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", ids).
			Order("id ASC").
			Find(v).Error
	})
	if err != nil || len(due) == 0 {
		return nil, err
	}
	dueIDs := make([]int64, 0, len(due))
	keys := make([]string, 0, len(due)*3)
	for _, sub := range due {
		dueIDs = append(dueIDs, sub.Id)
		keys = append(keys, sub.GetCacheKeys()...)
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&usersub.Subscribe{}).Where("id IN ?", dueIDs).Updates(map[string]any{
			"upload":           0,
			"download":         0,
			"status":           usersub.SubscribeStatusActive,
			"finished_at":      nil,
			"traffic_reset_at": day,
		}).Error
	}, keys...)
	if err != nil {
		return nil, err
	}
	for _, sub := range due {
		marker := day
		sub.Upload, sub.Download = 0, 0
		sub.Status, sub.FinishedAt, sub.TrafficResetAt = usersub.SubscribeStatusActive, nil, &marker
	}
	return due, nil
}

func subscribeFilterQuery(conn *gorm.DB, filter *usersub.SubscribeFilter) *gorm.DB {
	query := conn.Model(&usersub.Subscribe{})
	if filter == nil {
		return query
	}
	if len(filter.Subscribers) > 0 {
		query = query.Where("subscribe_id IN ?", filter.Subscribers)
	}
	if filter.IsActive != nil {
		if *filter.IsActive {
			query = query.Where("status IN ?", usersub.CurrentStatuses.Values())
		} else {
			query = query.Where("status IN ?", usersub.EndedStatuses.Values())
		}
	} else {
		// Deducted subscriptions were refunded/cancelled and must never receive
		// marketing quota grants, even when no activity filter was supplied.
		query = query.Where("status <> ?", usersub.SubscribeStatusDeducted)
	}
	if filter.StartTime != 0 {
		query = query.Where("start_time <= ?", time.UnixMilli(filter.StartTime))
	}
	if filter.EndTime != 0 {
		query = query.Where("expire_time >= ?", time.UnixMilli(filter.EndTime))
	}
	return query
}

func (m *UserSubscriptionRepo) QuerySubscribeIdsByFilter(ctx context.Context, filter *usersub.SubscribeFilter) ([]int64, error) {
	var ids []int64
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v any) error {
		return subscribeFilterQuery(conn, filter).Pluck("id", v).Error
	})
	return ids, err
}

func (m *UserSubscriptionRepo) CountSubscribesByFilter(ctx context.Context, filter *usersub.SubscribeFilter) (int64, error) {
	var total int64
	err := m.QueryNoCacheCtx(ctx, &total, func(conn *gorm.DB, v any) error {
		return subscribeFilterQuery(conn, filter).Count(&total).Error
	})
	return total, err
}

func (m *UserSubscriptionRepo) FindSubscribesByIds(ctx context.Context, ids []int64) ([]*usersub.Subscribe, error) {
	var subscribes []*usersub.Subscribe
	if len(ids) == 0 {
		return subscribes, nil
	}
	err := m.QueryNoCacheCtx(ctx, &subscribes, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).Where("id IN ?", ids).Find(&subscribes).Error
	})
	return subscribes, err
}

func (m *UserSubscriptionRepo) FindOneSubscribeDetailsById(ctx context.Context, id int64) (*usersub.SubscribeDetails, error) {
	var data usersub.SubscribeDetails
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		// Subscribe is a same-domain association; the identity-domain User
		// row is composed by the consumer at the module layer instead of a
		// cross-domain preload (ADR-001 step 5).
		return conn.Model(&usersub.Subscribe{}).Preload("Subscribe").Where("id = ?", id).First(&data).Error
	})
	return &data, err
}
