package coupon

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteCouponLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete coupon
func NewDeleteCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteCouponLogic {
	return &DeleteCouponLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteCouponLogic) DeleteCoupon(req *dto.DeleteCouponRequest) error {
	// delete coupon by id
	err := l.svcCtx.Store.Coupon().Delete(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[DeleteCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete coupon error: %v", err.Error())
	}
	return nil
}
