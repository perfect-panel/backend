package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	paymentPlatform "github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/orm"
	"gorm.io/gorm"
)

var (
	cachePaymentIdPrefix    = "cache:payment:id:"
	cachePaymentTokenPrefix = "cache:payment:token:"
)

var _ repository.PaymentRepo = (*paymentRepo)(nil)

type paymentRepo struct {
	cache.CachedConn
	table string
}

// NewPaymentRepo builds the module-owned implementation over the shared
// cached connection.
func NewPaymentRepo(conn cache.CachedConn) repository.PaymentRepo {
	return &paymentRepo{
		CachedConn: conn,
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

func (m *paymentRepo) Insert(ctx context.Context, data *payment.Payment) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
}

func (m *paymentRepo) FindOne(ctx context.Context, id int64) (*payment.Payment, error) {
	var resp payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&payment.Payment{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// paymentEditableColumns are the columns an administrator's edit writes. The
// platform is fixed at creation and the notify token never changes: a
// whole-row save could rewrite both from a stale or forged row.
var paymentEditableColumns = []string{"name", "icon", "domain", "config", "description", "fee_mode", "fee_percent", "fee_amount", "sort", "enable"}

// Update writes the editable columns of the payment method. Enable must be
// set: a nil value would clear the column, so the caller defaults it first.
func (m *paymentRepo) Update(ctx context.Context, data *payment.Payment) error {
	if data == nil || data.Id == 0 {
		return errors.New("payment method update needs the method id")
	}
	if data.Enable == nil {
		return errors.New("payment method update needs the enable flag")
	}
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&payment.Payment{}).Where("id = ?", data.Id).Select(paymentEditableColumns).Updates(data).Error
	}, m.getCacheKeys(old)...)
}

func (m *paymentRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		// Delete the loaded row, not a zero value: the entity's BeforeDelete
		// guard inspects the id of the method being deleted.
		return conn.Delete(data).Error
	}, m.getCacheKeys(data)...)
}

func (m *paymentRepo) FindOneByPaymentToken(ctx context.Context, token string) (*payment.Payment, error) {
	var resp *payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&payment.Payment{}).Where("token = ?", token).First(v).Error
	})
	return resp, err
}

func (m *paymentRepo) FindAll(ctx context.Context) ([]*payment.Payment, error) {
	var resp []*payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&payment.Payment{}).Order("sort ASC, id ASC").Find(v).Error
	})
	return resp, err
}

func (m *paymentRepo) FindAvailableMethods(ctx context.Context) ([]*payment.Payment, error) {
	var resp []*payment.Payment
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
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
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
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
