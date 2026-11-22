// Package repo holds the platform module's repository implementations: the
// system settings, cached in Redis, the system log, the task bookkeeping and
// the event inbox and outbox. The module facade exports them through
// NewRepoBuilder.
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository/kernel"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/cache"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	cacheSystemIdPrefix  = "cache:System:id:"
	cacheSystemKeyPrefix = "cache:System:key:"
)

var _ kernel.SystemRepo = (*systemRepo)(nil)

type systemRepo struct {
	cache.CachedConn
	table string
}

// NewSystemRepo builds the module-owned implementation over the shared
// cached connection.
func NewSystemRepo(conn cache.CachedConn) kernel.SystemRepo {
	return &systemRepo{
		CachedConn: conn,
		table:      "System",
	}
}

func (m *systemRepo) getCacheKeys(data *system.System) []string {
	if data == nil {
		return []string{}
	}
	keys := []string{fmt.Sprintf("%s%v", cacheSystemIdPrefix, data.Id)}
	if data.Key != "" {
		keys = append(keys, fmt.Sprintf("%s%v", cacheSystemKeyPrefix, data.Key))
	}
	return append(keys, systemCategoryCacheKeys(data.Category)...)
}

func systemCategoryCacheKeys(category string) []string {
	switch category {
	case "sms":
		return []string{config.SmsConfigKey}
	case "site":
		return []string{config.SiteConfigKey, config.GlobalConfigKey}
	case "email":
		return []string{config.EmailSmtpConfigKey}
	case "subscribe":
		return []string{config.SubscribeConfigKey, config.GlobalConfigKey}
	case "register":
		return []string{config.RegisterConfigKey, config.GlobalConfigKey}
	case "verify":
		return []string{config.VerifyConfigKey, config.GlobalConfigKey}
	case "server":
		return []string{config.NodeConfigKey}
	case "invite":
		return []string{config.InviteConfigKey, config.GlobalConfigKey}
	case "telegram":
		return []string{config.TelegramConfigKey}
	case "tos":
		return []string{config.TosConfigKey}
	case "currency":
		return []string{config.CurrencyConfigKey, config.GlobalConfigKey}
	case "verify_code":
		return []string{config.VerifyCodeConfigKey}
	default:
		return nil
	}
}

func (m *systemRepo) FindOneByKey(ctx context.Context, key string) (*system.System, error) {
	sys := new(system.System)
	cacheKey := fmt.Sprintf("%s%v", cacheSystemKeyPrefix, key)
	err := m.QueryCtx(ctx, sys, cacheKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&system.System{}).Scopes(systemWhereKey(key)).First(v).Error
	})
	return sys, err
}

func (m *systemRepo) Insert(ctx context.Context, data *system.System) error {
	err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *systemRepo) FindOne(ctx context.Context, id int64) (*system.System, error) {
	SystemIdKey := fmt.Sprintf("%s%v", cacheSystemIdPrefix, id)
	var resp system.System
	err := m.QueryCtx(ctx, &resp, SystemIdKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&system.System{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Update rewrites the setting's mutable columns from data. It never inserts
// a row and never touches the creation time, which a whole-row save would.
func (m *systemRepo) Update(ctx context.Context, data *system.System) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	keys := append(m.getCacheKeys(old), m.getCacheKeys(data)...)
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&system.System{}).Where("id = ?", data.Id).
			Select("category", "key", "value", "type", "desc").Updates(data).Error
	}, keys...)
	return err
}

func (m *systemRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		return db.Delete(&system.System{}, id).Error
	}, m.getCacheKeys(data)...)
	return err
}

// GetSmsConfig returns the sms config.
func (m *systemRepo) GetSmsConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "sms", config.SmsConfigKey)
}

// GetSiteConfig returns the site config.
func (m *systemRepo) GetSiteConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "site", config.SiteConfigKey)
}

// GetEmailConfig returns the email config.
func (m *systemRepo) GetEmailConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "email", config.EmailSmtpConfigKey)
}

// GetSubscribeConfig returns the subscribe config.
func (m *systemRepo) GetSubscribeConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "subscribe", config.SubscribeConfigKey)
}

// GetRegisterConfig returns the register config.
func (m *systemRepo) GetRegisterConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "register", config.RegisterConfigKey)
}

// GetVerifyConfig returns the verify config.
func (m *systemRepo) GetVerifyConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "verify", config.VerifyConfigKey)
}

// GetNodeConfig returns the server config.
func (m *systemRepo) GetNodeConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "server", config.NodeConfigKey)
}

// GetInviteConfig returns the invite config.
func (m *systemRepo) GetInviteConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "invite", config.InviteConfigKey)
}

// GetTelegramConfig returns the telegram config.
func (m *systemRepo) GetTelegramConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "telegram", config.TelegramConfigKey)
}

// GetTosConfig returns the tos config.
func (m *systemRepo) GetTosConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "tos", config.TosConfigKey)
}

// GetCurrencyConfig returns the currency config.
func (m *systemRepo) GetCurrencyConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "currency", config.CurrencyConfigKey)
}

func (m *systemRepo) UpdateValueByCategoryKey(ctx context.Context, category, key, value string, valueType ...string) error {
	err := m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&system.System{}).
			Scopes(systemWhereCategoryKey(category, key)).
			Update("value", value)
		if result.Error != nil || result.RowsAffected > 0 {
			return result.Error
		}
		fieldType := "string"
		if len(valueType) > 0 && valueType[0] != "" {
			fieldType = valueType[0]
		}
		return conn.Create(&system.System{
			Category: category,
			Key:      key,
			Value:    value,
			Type:     fieldType,
			Desc:     key,
		}).Error
	})
	if err != nil {
		return err
	}
	// Cache invalidation is best-effort and is deferred automatically when the
	// repository is used through Store.InTx.
	_ = m.DelCacheCtx(ctx, append([]string{fmt.Sprintf("%s%v", cacheSystemKeyPrefix, key)}, systemCategoryCacheKeys(category)...)...)
	return nil
}

func (m *systemRepo) UpdateNodeMultiplierConfig(ctx context.Context, config string) error {
	err := m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&system.System{}).
			Scopes(systemWhereCategoryKey("server", "NodeMultiplierConfig")).
			Update("value", config).Error
	})
	if err != nil {
		return err
	}
	_ = m.DelCacheCtx(ctx, systemCategoryCacheKeys("server")...)
	return nil
}

func (m *systemRepo) FindNodeMultiplierConfig(ctx context.Context) (*system.System, error) {
	var data system.System
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Scopes(systemWhereCategoryKey("server", "NodeMultiplierConfig")).Find(v).Error
	})
	return &data, err
}

// GetVerifyCodeConfig returns the verify code config.
func (m *systemRepo) GetVerifyCodeConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "verify_code", config.VerifyCodeConfigKey)
}

// GetLogConfig returns the log config; it is read uncached.
func (m *systemRepo) GetLogConfig(ctx context.Context) ([]*system.System, error) {
	return m.categoryConfig(ctx, "log", "")
}

// categoryConfig returns the settings rows of category, cached under
// cacheKey; an empty cacheKey reads the database every time.
func (m *systemRepo) categoryConfig(ctx context.Context, category, cacheKey string) ([]*system.System, error) {
	var configs []*system.System
	query := func(conn *gorm.DB, v any) error {
		return conn.Where("category = ?", category).Find(v).Error
	}
	var err error
	if cacheKey == "" {
		err = m.QueryNoCacheCtx(ctx, &configs, query)
	} else {
		err = m.QueryCtx(ctx, &configs, cacheKey, query)
	}
	return configs, err
}

// systemWhereKey returns a GORM scope filtering by the "key" column.
func systemWhereKey(key string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(clause.Eq{
			Column: clause.Column{Name: "key"},
			Value:  key,
		})
	}
}

// systemWhereCategoryKey returns a GORM scope filtering by both "category" and "key".
func systemWhereCategoryKey(category, key string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(clause.Eq{
			Column: clause.Column{Name: "category"},
			Value:  category,
		}).Where(clause.Eq{
			Column: clause.Column{Name: "key"},
			Value:  key,
		})
	}
}
