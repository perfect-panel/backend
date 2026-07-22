package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/orm"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	cachePaymentIdPrefix    = "cache:payment:id:"
	cachePaymentTokenPrefix = "cache:payment:token:"
)

// PaymentRepo payment 数据访问接口
type PaymentRepo interface {
	Insert(ctx context.Context, data *payment.Payment, tx ...*gorm.DB) error
	FindOne(ctx context.Context, id int64) (*payment.Payment, error)
	Update(ctx context.Context, data *payment.Payment, tx ...*gorm.DB) error
	Delete(ctx context.Context, id int64, tx ...*gorm.DB) error
	FindOneByPaymentToken(ctx context.Context, token string) (*payment.Payment, error)
	FindAll(ctx context.Context) ([]*payment.Payment, error)
	FindListByPage(ctx context.Context, page, size int, req *payment.Filter) (int64, []*payment.Payment, error)
	FindAvailableMethods(ctx context.Context) ([]*payment.Payment, error)
}

var _ PaymentRepo = (*paymentRepo)(nil)

type paymentRepo struct {
	cache.CachedConn
	table string
}

func newPaymentRepo(db *gorm.DB, c *redis.Client, invalidations ...*cache.InvalidationQueue) PaymentRepo {
	return &paymentRepo{
		CachedConn: newCachedConn(db, c, invalidations...),
		table:      "Payment",
	}
}

func (m *paymentRepo) getCacheKeys(data *payment.Payment) []string {
	if data == nil {
		return []string{}
	}
	paymentIdKey := fmt.Sprintf("%s%v", cachePaymentIdPrefix, data.Id)
	paymentNameKey := fmt.Sprintf("%s%v", cachePaymentTokenPrefix, data.Token)
	return []string{
		paymentIdKey,
		paymentNameKey,
	}
}

func (m *paymentRepo) Insert(ctx context.Context, data *payment.Payment, tx ...*gorm.DB) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
}

func (m *paymentRepo) FindOne(ctx context.Context, id int64) (*payment.Payment, error) {
	var resp payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&payment.Payment{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *paymentRepo) Update(ctx context.Context, data *payment.Payment, tx ...*gorm.DB) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Save(data).Error
	}, m.getCacheKeys(old)...)
}

func (m *paymentRepo) Delete(ctx context.Context, id int64, tx ...*gorm.DB) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Delete(&payment.Payment{}, id).Error
	}, m.getCacheKeys(data)...)
}

func (m *paymentRepo) FindOneByPaymentToken(ctx context.Context, token string) (*payment.Payment, error) {
	var resp *payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&payment.Payment{}).Where("token = ?", token).First(v).Error
	})
	return resp, err
}

func (m *paymentRepo) FindAll(ctx context.Context) ([]*payment.Payment, error) {
	var resp []*payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&payment.Payment{}).Order("sort ASC, id ASC").Find(v).Error
	})
	return resp, err
}

func (m *paymentRepo) FindAvailableMethods(ctx context.Context) ([]*payment.Payment, error) {
	var resp []*payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		// Legacy rows for removed or otherwise unsupported gateways must never
		// be offered to a buyer, even if they remain enabled in the database.
		return conn.Model(&payment.Payment{}).
			Where("enable = ? AND platform IN ?", true, paymentPlatform.SupportedPlatformNames()).
			Order("sort ASC, id ASC").
			Find(v).Error
	})
	return resp, err
}

func (m *paymentRepo) FindListByPage(ctx context.Context, page, size int, req *payment.Filter) (int64, []*payment.Payment, error) {
	var resp []*payment.Payment
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&payment.Payment{})
		if req != nil {
			if req.Enable != nil {
				conn = conn.Where("enable = ?", *req.Enable)
			}
			if req.Mark != "" {
				conn = conn.Where("platform = ?", req.Mark)
			}
			if req.Search != "" {
				conn = conn.Scopes(orm.PrefixLike([]string{"name"}, req.Search))
			}
		}
		return conn.Count(&total).Order("sort ASC, id ASC").Offset((page - 1) * size).Limit(size).Find(v).Error
	})
	return total, resp, err
}
