package coupon

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type BatchDeleteCouponLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete coupon
func NewBatchDeleteCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteCouponLogic {
	return &BatchDeleteCouponLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteCouponLogic) BatchDeleteCoupon(req *dto.BatchDeleteCouponRequest) error {
	// batch delete coupon by ids
	err := l.svcCtx.Store.Coupon().BatchDelete(l.ctx, req.Ids)
	if err != nil {
		l.Errorw("[BatchDeleteCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete coupon error: %v", err.Error())
	}
	return nil
}
