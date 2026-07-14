package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/perfect-panel/server/internal/model/entity/coupon"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	cacheCouponIdPrefix   = "cache:coupon:id:"
	cacheCouponCodePrefix = "cache:coupon:code:"
)

// CouponRepo coupon 数据访问接口
type CouponRepo interface {
	Insert(ctx context.Context, data *coupon.Coupon) error
	FindOne(ctx context.Context, id int64) (*coupon.Coupon, error)
	FindOneByCode(ctx context.Context, code string) (*coupon.Coupon, error)
	Update(ctx context.Context, data *coupon.Coupon) error
	Delete(ctx context.Context, id int64) error
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
	UpdateCount(ctx context.Context, code string) error
	QueryCouponListByPage(ctx context.Context, page, size int, subscribe int64, search string) (total int64, list []*coupon.Coupon, err error)
	BatchDelete(ctx context.Context, ids []int64) error
}

var _ CouponRepo = (*couponRepo)(nil)

type couponRepo struct {
	cache.CachedConn
	table string
}

func newCouponRepo(db *gorm.DB, c *redis.Client) CouponRepo {
	return &couponRepo{
		CachedConn: cache.NewConn(db, c),
		table:      "coupon",
	}
}

//nolint:unused
func (m *couponRepo) batchGetCacheKeys(Coupons ...*coupon.Coupon) []string {
	var keys []string
	for _, coupon := range Coupons {
		keys = append(keys, m.getCacheKeys(coupon)...)
	}
	return keys

}

func (m *couponRepo) getCacheKeys(data *coupon.Coupon) []string {
	if data == nil {
		return []string{}
	}
	couponIdKey := fmt.Sprintf("%s%v", cacheCouponIdPrefix, data.Id)
	couponCodeKey := fmt.Sprintf("%s%v", cacheCouponCodePrefix, data.Code)
	cacheKeys := []string{
		couponIdKey,
		couponCodeKey,
	}
	return cacheKeys
}

func (m *couponRepo) Insert(ctx context.Context, data *coupon.Coupon) error {
	err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *couponRepo) FindOne(ctx context.Context, id int64) (*coupon.Coupon, error) {
	var resp coupon.Coupon
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&coupon.Coupon{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *couponRepo) FindOneByCode(ctx context.Context, code string) (*coupon.Coupon, error) {
	var resp coupon.Coupon
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&coupon.Coupon{}).Where("code = ?", code).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *couponRepo) Update(ctx context.Context, data *coupon.Coupon) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		return db.Save(data).Error
	}, m.getCacheKeys(old)...)
	return err
}

func (m *couponRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		return db.Delete(&coupon.Coupon{}, id).Error
	}, m.getCacheKeys(data)...)
	return err
}

func (m *couponRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.TransactCtx(ctx, fn)
}

// QueryCouponListByPage query coupon list by page
func (m *couponRepo) QueryCouponListByPage(ctx context.Context, page, size int, subscribe int64, search string) (total int64, list []*coupon.Coupon, err error) {
	page, size = normalizePage(page, size)
	err = m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		db := conn.Model(&coupon.Coupon{})
		if subscribe != 0 {
			db = db.Scopes(orm.CommaSeparatedContains("subscribe", []string{strconv.FormatInt(subscribe, 10)}))
		}
		if search != "" {
			db = db.Scopes(orm.PrefixLike([]string{"name", "code"}, search))
		}
		return db.Count(&total).Limit(size).Offset((page - 1) * size).Find(v).Error
	})
	return total, list, err
}

func (m *couponRepo) BatchDelete(ctx context.Context, ids []int64) error {
	var err error
	for _, id := range ids {
		if err = m.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (m *couponRepo) UpdateCount(ctx context.Context, code string) error {
	data, err := m.FindOneByCode(ctx, code)
	if err != nil {
		return err
	}
	data.UsedCount++
	return m.Update(ctx, data)
}
