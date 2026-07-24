// Package coupon implements the coupon management subdomain of the billing
// module. Only the module facade may reach it.
package coupon

import (
	"context"
	"math/rand"

	"github.com/perfect-panel/server/internal/model/dto"
	entity "github.com/perfect-panel/server/internal/model/entity/coupon"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/snowflake"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type Service struct {
	repo repository.CouponRepo
}

func NewService(repo repository.CouponRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req *dto.CreateCouponRequest) error {
	if err := validateCouponInput(req); err != nil {
		return err
	}
	if req.Code == "" {
		rand.NewSource(timeutil.Now().UnixNano())
		sid := snowflake.GetID()
		req.Code = random.KeyNew(4, 2) + "-" + random.StrToDashedString(random.EncodeBase36(sid))
	}
	couponInfo := &entity.Coupon{}
	tool.DeepCopy(couponInfo, req)
	couponInfo.Subscribe = tool.Int64SliceToString(req.Subscribe)
	if req.Enable == nil {
		enabled := true
		couponInfo.Enable = &enabled
	}
	if err := s.repo.Insert(ctx, couponInfo); err != nil {
		logger.WithContext(ctx).Errorw("[CreateCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create coupon error: %v", err.Error())
	}
	return nil
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
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err)
	}
	if req.UsedCount < existing.UsedCount {
		return errors.Wrapf(xerr.NewErrCodeMsg(400, "COUPON_USED_COUNT_IMMUTABLE"), "used count cannot be reduced")
	}
	couponInfo := &entity.Coupon{}
	tool.DeepCopy(couponInfo, req)
	couponInfo.Subscribe = tool.Int64SliceToString(req.Subscribe)
	if couponInfo.Enable == nil {
		couponInfo.Enable = existing.Enable
	}
	if err := s.repo.Update(ctx, couponInfo); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update coupon error: %v", err.Error())
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, req *dto.DeleteCouponRequest) error {
	if err := s.repo.Delete(ctx, req.Id); err != nil {
		logger.WithContext(ctx).Errorw("[DeleteCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete coupon error: %v", err.Error())
	}
	return nil
}

func (s *Service) BatchDelete(ctx context.Context, req *dto.BatchDeleteCouponRequest) error {
	if err := s.repo.BatchDelete(ctx, req.Ids); err != nil {
		logger.WithContext(ctx).Errorw("[BatchDeleteCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete coupon error: %v", err.Error())
	}
	return nil
}

func (s *Service) List(ctx context.Context, req *dto.GetCouponListRequest) (*dto.GetCouponListResponse, error) {
	resp := &dto.GetCouponListResponse{}
	total, list, err := s.repo.QueryCouponListByPage(ctx, int(req.Page), int(req.Size), req.Subscribe, req.Search)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetCouponList] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get coupon list error: %v", err.Error())
	}
	resp.Total = total
	resp.List = make([]dto.Coupon, 0)
	for _, item := range list {
		couponInfo := dto.Coupon{}
		tool.DeepCopy(&couponInfo, item)
		couponInfo.Subscribe = tool.StringToInt64Slice(item.Subscribe)
		resp.List = append(resp.List, couponInfo)
	}
	return resp, nil
}

func validateCouponInput(req *dto.CreateCouponRequest) error {
	if req.Count < 0 || req.UsedCount < 0 || req.UserLimit < 0 || req.StartTime <= 0 || req.ExpireTime <= req.StartTime {
		return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_COUPON"), "invalid coupon limits or validity window")
	}
	if req.Count > 0 && req.UsedCount > req.Count {
		return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_COUPON"), "used count exceeds coupon count")
	}
	switch req.Type {
	case 1:
		if req.Discount <= 0 || req.Discount > 100 {
			return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_COUPON_DISCOUNT"), "percentage discount must be between 1 and 100")
		}
	case 2:
		if req.Discount <= 0 {
			return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_COUPON_DISCOUNT"), "fixed discount must be positive")
		}
	default:
		return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_COUPON_TYPE"), "unsupported coupon type")
	}
	return nil
}
