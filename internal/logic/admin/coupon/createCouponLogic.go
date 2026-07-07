package coupon

import (
	"context"
	"math/rand"

	"github.com/perfect-panel/server/internal/model/coupon"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/snowflake"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateCouponLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create coupon
func NewCreateCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCouponLogic {
	return &CreateCouponLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateCouponLogic) CreateCoupon(req *types.CreateCouponRequest) error {
	if req.Code == "" {
		rand.NewSource(timeutil.Now().UnixNano())
		sid := snowflake.GetID()
		req.Code = random.KeyNew(4, 2) + "-" + random.StrToDashedString(random.EncodeBase36(sid))
	}
	couponInfo := &coupon.Coupon{}
	tool.DeepCopy(couponInfo, req)
	couponInfo.Subscribe = tool.Int64SliceToString(req.Subscribe)
	enabled := true
	couponInfo.Enable = &enabled
	err := l.svcCtx.Store.Coupon().Insert(l.ctx, couponInfo)
	if err != nil {
		l.Errorw("[CreateCoupon] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create coupon error: %v", err.Error())
	}
	return nil
}
