package coupon

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/coupon"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateCouponLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update coupon
func NewUpdateCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCouponLogic {
	return &UpdateCouponLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateCouponLogic) UpdateCoupon(req *dto.UpdateCouponRequest) error {
	fmt.Printf("req Subscribe: %v\n", req.Subscribe)
	couponInfo := &coupon.Coupon{}
	// update coupon
	tool.DeepCopy(couponInfo, req)
	couponInfo.Subscribe = tool.Int64SliceToString(req.Subscribe)
	err := l.svcCtx.Store.Coupon().Update(l.ctx, couponInfo)
	if err != nil {
		l.Errorw("[UpdateCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update coupon error: %v", err.Error())
	}
	return nil
}
