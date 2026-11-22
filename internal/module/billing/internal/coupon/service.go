// Package coupon implements the coupon management subdomain of the billing
// module. Only the module facade may reach it.
package coupon

import (
	"context"
	"crypto/rand"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	entity "github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// Coupons is the coupon persistence the administration uses; the coupon
// repository satisfies it.
type Coupons interface {
	Insert(ctx context.Context, data *entity.Coupon) error
	FindOne(ctx context.Context, id int64) (*entity.Coupon, error)
	Update(ctx context.Context, data *entity.Coupon) error
	Delete(ctx context.Context, id int64) error
	BatchDelete(ctx context.Context, ids []int64) error
	QueryCouponListByPage(ctx context.Context, page, size int, subscribe int64, search string) (total int64, list []*entity.Coupon, err error)
}

// Service is the coupon administration used by the billing facade.
type Service struct {
	repo Coupons
}

func NewService(repo Coupons) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req *dto.CreateCouponRequest) error {
	if err := validateCouponInput(req); err != nil {
		return err
	}
	generateCode := req.Code == ""
	couponInfo := couponRow(req)
	if req.Enable == nil {
		enabled := true
		couponInfo.Enable = &enabled
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if generateCode {
			// Coupon codes need unpredictable uniqueness, not a machine/clock
			// based numeric ID. Keep the database's unique constraint authoritative.
			req.Code = random.StrToDashedString(rand.Text())
			couponInfo.Code = req.Code
			couponInfo.Id = 0
		}
		err = s.repo.Insert(ctx, couponInfo)
		if err == nil {
			return nil
		}
		if !generateCode || !errors.Is(err, gorm.ErrDuplicatedKey) {
			break
		}
	}
	return xerr.Wrapf(err, xerr.DatabaseInsertError, "create coupon")
}

func (s *Service) Update(ctx context.Context, req *dto.UpdateCouponRequest) error {
	input := &dto.CreateCouponRequest{
		Name: req.Name, Code: req.Code, Count: req.Count, Type: req.Type,
		Discount: req.Discount, StartTime: req.StartTime, ExpireTime: req.ExpireTime,
		UserLimit: req.UserLimit, Subscribe: req.Subscribe, UsedCount: req.UsedCount, Enable: req.Enable,
	}
	if err := validateCouponInput(input); err != nil {
		return err
	}
	existing, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find coupon %d", req.Id)
	}
	if req.UsedCount < existing.UsedCount {
		return xerr.Errorf(xerr.CouponUsedCountImmutable, "used count cannot be reduced")
	}
	couponInfo := couponRow(input)
	couponInfo.Id = req.Id
	if couponInfo.Enable == nil {
		couponInfo.Enable = existing.Enable
	}
	if err := s.repo.Update(ctx, couponInfo); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update coupon %d", req.Id)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, req *dto.DeleteCouponRequest) error {
	if err := s.repo.Delete(ctx, req.Id); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete coupon %d", req.Id)
	}
	return nil
}

func (s *Service) BatchDelete(ctx context.Context, req *dto.BatchDeleteCouponRequest) error {
	if err := s.repo.BatchDelete(ctx, req.Ids); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "batch delete coupons")
	}
	return nil
}

func (s *Service) List(ctx context.Context, req *dto.GetCouponListRequest) (*dto.GetCouponListResponse, error) {
	resp := &dto.GetCouponListResponse{}
	total, list, err := s.repo.QueryCouponListByPage(ctx, int(req.Page), int(req.Size), req.Subscribe, req.Search)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get coupon list")
	}
	resp.Total = total
	resp.List = make([]dto.Coupon, 0)
	for _, item := range list {
		plans, parseErr := slicesx.ParseInt64CSV(item.Subscribe)
		if parseErr != nil {
			// A damaged plan list must not hide the whole page: the row is
			// listed without plans so the administrator can find and repair
			// it. Checkout keeps refusing the coupon meanwhile, as pricing
			// reads the same column fail-closed.
			logger.WithContext(ctx).Errorw("[GetCouponList] coupon plan list is damaged",
				logger.Field("coupon_id", item.Id), logger.Field("error", parseErr.Error()))
			plans = nil
		}
		resp.List = append(resp.List, dto.Coupon{
			Id:         item.Id,
			Name:       item.Name,
			Code:       item.Code,
			Count:      item.Count,
			Type:       item.Type,
			Discount:   item.Discount,
			StartTime:  item.StartTime,
			ExpireTime: item.ExpireTime,
			UserLimit:  item.UserLimit,
			Subscribe:  plans,
			UsedCount:  item.UsedCount,
			Enable:     item.IsEnabled(),
			CreatedAt:  item.CreatedAt.UnixMilli(),
			UpdatedAt:  item.UpdatedAt.UnixMilli(),
		})
	}
	return resp, nil
}

// couponRow is the stored form of a coupon request: the plans it is limited
// to become the comma-separated column. A nil Enable is left for the caller
// to default.
func couponRow(req *dto.CreateCouponRequest) *entity.Coupon {
	return &entity.Coupon{
		Name:       req.Name,
		Code:       req.Code,
		Count:      req.Count,
		Type:       req.Type,
		Discount:   req.Discount,
		StartTime:  req.StartTime,
		ExpireTime: req.ExpireTime,
		UserLimit:  req.UserLimit,
		Subscribe:  slicesx.Int64SliceToString(req.Subscribe),
		UsedCount:  req.UsedCount,
		Enable:     req.Enable,
	}
}

func validateCouponInput(req *dto.CreateCouponRequest) error {
	if req.Count < 0 || req.UsedCount < 0 || req.UserLimit < 0 || req.StartTime <= 0 || req.ExpireTime <= req.StartTime {
		return xerr.Errorf(xerr.InvalidCoupon, "invalid coupon limits or validity window")
	}
	if req.Count > 0 && req.UsedCount > req.Count {
		return xerr.Errorf(xerr.InvalidCoupon, "used count exceeds coupon count")
	}
	switch req.Type {
	case entity.TypePercentage:
		if req.Discount <= 0 || req.Discount > 100 {
			return xerr.Errorf(xerr.InvalidCouponDiscount, "percentage discount must be between 1 and 100")
		}
	case entity.TypeFixed:
		if req.Discount <= 0 {
			return xerr.Errorf(xerr.InvalidCouponDiscount, "fixed discount must be positive")
		}
	default:
		return xerr.Errorf(xerr.InvalidCouponType, "unsupported coupon type")
	}
	return nil
}
